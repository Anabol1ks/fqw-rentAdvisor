package cian

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"skripe/internal/repo"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/extensions"
	"github.com/gocolly/colly/v2/proxy"
	"go.uber.org/zap"
)

type Options struct {
	DBRepo               *repo.RawRepository
	StartURLTmpl         string // например: https://www.cian.ru/cat.php?deal_type=sale&engine_version=2&p={page}&region=1
	City                 string
	StartPage            int
	Pages                int
	SingleURL            string // если задан, обойти только этот URL карточки
	SnapshotDir          string
	ProxyURL             string // опционально
	DelayMin             time.Duration
	DelayMax             time.Duration
	Parallelism          int
	Cookie               string
	UseReferer           bool
	MaxItems             int
	MaxEmptyListingPages int
	DealType             string // "sale" | "rent_long" | "rent_daily"
}

var detailRe = regexp.MustCompile(`/\d{7,10}/`) // URL карточек на cian.ru часто /sale/flat/314553642/
// Строгая проверка детальной карточки: только /rent/flat/<id> или /sale/flat/<id>
var detailPageRe = regexp.MustCompile(`^https?://(?:www\.)?cian\.ru/(?:rent|sale)/flat/\d{6,}(?:/)?(?:\?.*)?$`)

func (o Options) buildPageURL(page int) string {
	u := strings.ReplaceAll(o.StartURLTmpl, "{city}", url.PathEscape(o.City))
	u = strings.ReplaceAll(u, "{page}", fmt.Sprintf("%d", page))
	return u
}

func Run(o Options, log *zap.Logger) error {
	startTS := time.Now()
	log.Info("cian scrape start",
		zap.String("deal", o.DealType),
		zap.String("city", o.City),
		zap.Int("start_page", o.StartPage),
		zap.Int("pages", o.Pages),
		zap.Int("parallel", o.Parallelism),
		zap.Duration("delay_min", o.DelayMin),
		zap.Duration("delay_max", o.DelayMax),
		zap.Int("max_items", o.MaxItems),
		zap.Int("max_empty_pages", o.MaxEmptyListingPages),
		zap.Bool("use_referer", o.UseReferer),
	)

	c := colly.NewCollector(
		colly.AllowedDomains("cian.ru", "www.cian.ru"),
		colly.Async(true),
		colly.MaxDepth(2),
	)
	c.SetRequestTimeout(clampDuration(o.DelayMax+10*time.Second, 5*time.Second, 60*time.Second))
	extensions.RandomUserAgent(c)
	if o.UseReferer {
		extensions.Referer(c)
	}
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: max(1, o.Parallelism),
		RandomDelay: clampDuration(o.DelayMax, 0, 10*time.Second),
		Delay:       clampDuration(o.DelayMin, 0, 5*time.Second),
	})
	if o.ProxyURL != "" {
		if pf, err := proxy.RoundRobinProxySwitcher(o.ProxyURL); err == nil {
			c.SetProxyFunc(pf)
		} else {
			log.Warn("proxy init failed", zap.Error(err))
		}
	}

	var upserted int64
	var shouldStop int64
	var stopReason atomic.Value // string
	var inFlight int64
	var mu sync.Mutex
	listingLinkCount := make(map[string]int)
	currentPage := o.StartPage
	pagesVisited := 0
	emptyStreak := 0

	// 1) Собираем ссылки на карточки с выдачи
	if strings.TrimSpace(o.SingleURL) == "" {
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			href := strings.TrimSpace(e.Attr("href"))
			if href == "" {
				return
			}
			// Нормализуем относительные ссылки
			u := e.Request.AbsoluteURL(href)
			if u == "" {
				return
			}
			if !strings.Contains(u, "cian.ru") {
				return
			}
			// Отбросим нецелевые URL (pdf-экспорт, карта и пр.)
			if strings.Contains(u, "/export/pdf/") || strings.Contains(u, "/pdf/") {
				return
			}
			// Разрешаем только карточки /rent/flat/<id> или /sale/flat/<id>
			if !detailPageRe.MatchString(u) {
				return
			}
			if atomic.LoadInt64(&shouldStop) != 0 {
				return
			}
			atomic.AddInt64(&inFlight, 1)
			_ = e.Request.Visit(u)
			// учтём линк
			mu.Lock()
			listingLinkCount[e.Request.URL.String()]++
			mu.Unlock()
		})
	}

	// 2) Парс детальной карточки
	c.OnResponse(func(r *colly.Response) {
		defer atomic.AddInt64(&inFlight, -1)

		if o.MaxItems > 0 && atomic.LoadInt64(&upserted) >= int64(o.MaxItems) {
			return
		}
		if !strings.Contains(r.Headers.Get("Content-Type"), "text/html") {
			return
		}
		// карточка?
		if !detailPageRe.MatchString(r.Request.URL.String()) {
			// это страница выдачи
			log.Info("serp page loaded", zap.String("url", r.Request.URL.String()))
			return
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(r.Body))
		if err != nil {
			log.Warn("goquery doc", zap.Error(err))
			return
		}
		log.Info("detail loaded", zap.String("url", r.Request.URL.String()))
		jld, _ := parseJSONLD(doc)

		// Адрес/координаты: JSON-LD → DOM/inline JSON
		addrParts := make([]string, 0, 2)
		if strings.TrimSpace(jld.Address.StreetAddress) != "" {
			addrParts = append(addrParts, strings.TrimSpace(jld.Address.StreetAddress))
		}
		if strings.TrimSpace(jld.Address.AddressLocal) != "" {
			addrParts = append(addrParts, strings.TrimSpace(jld.Address.AddressLocal))
		}
		addrText := strings.Join(addrParts, ", ")
		lat := jld.Geo.Latitude
		lon := jld.Geo.Longitude
		if addrText == "" || lat == nil || lon == nil {
			a2, la2, lo2 := extractAddressAndPointFromHTML(string(r.Body))
			if addrText == "" && a2 != "" {
				addrText = a2
			}
			if lat == nil {
				lat = la2
			}
			if lon == nil {
				lon = lo2
			}
		}
		// Фолбэк: попробуем достать адрес из DOM/Title, если ещё пусто
		if strings.TrimSpace(addrText) == "" {
			if v := extractAddressFromDoc(doc); v != "" {
				addrText = v
			} else if v := extractAddressFromTitle(doc); v != "" {
				addrText = v
			}
		}

		pageURL := r.Request.URL.String()
		externalID := extractIDFromURL(pageURL)
		log.Info("parsing detail",
			zap.String("id", externalID),
			zap.String("url", pageURL),
		)

		// Определим тип сделки по URL карточки (перебьём общий, если надо)
		cardDealType := o.DealType
		lu := strings.ToLower(pageURL)
		if strings.Contains(lu, "/rent/") || strings.Contains(lu, "deal_type=rent") {
			cardDealType = "rent_long"
		} else if strings.Contains(lu, "/sale/") || strings.Contains(lu, "deal_type=sale") {
			cardDealType = "sale"
		}

		// Факты
		rRooms, rAreaTotal, rFloor, rFloors := extractFactsFromText(jld.Name, jld.Description)
		// Если этаж/этажность отсутствуют — сначала попробуем из inline JSON, потом DOM
		if rFloor == nil || rFloors == nil {
			f1, f2 := extractFloorsFromInlineJSON(string(r.Body))
			if rFloor == nil {
				rFloor = f1
			}
			if rFloors == nil {
				rFloors = f2
			}
		}
		if rFloor == nil || rFloors == nil {
			f1, f2 := extractFloorFromDoc(doc)
			if rFloor == nil {
				rFloor = f1
			}
			if rFloors == nil {
				rFloors = f2
			}
		}
		// Метро: стараемся получить формат "<Название> <N> мин" без префикса "м."
		metro := ""
		if name, mins := extractClosestMetroFromUndergroundList(doc); name != "" {
			if mins != nil {
				metro = strings.TrimSpace(name + " " + fmt.Sprintf("%d", *mins) + " мин")
			} else {
				metro = name
			}
		}
		if strings.TrimSpace(metro) == "" {
			if name, mins := extractClosestMetroFromInlineJSON(string(r.Body)); name != "" {
				if mins != nil {
					metro = strings.TrimSpace(name + " " + fmt.Sprintf("%d", *mins) + " мин")
				} else {
					metro = name
				}
			}
		}
		if strings.TrimSpace(metro) == "" {
			metro = extractMetroFromDoc(doc)
		}
		rAreaLiving, rAreaKitchen := extractAreasFromDoc(doc)
		yearBuilt, houseMaterial := extractHouseInfoFromDoc(doc)

		item := repo.RawItem{
			Source:        "cian",
			DealType:      cardDealType,
			ExternalID:    externalID,
			URL:           pageURL,
			Title:         strings.TrimSpace(jld.Name),
			Description:   strings.TrimSpace(jld.Description),
			AddressText:   strings.TrimSpace(strings.Trim(addrText, ", ")),
			PriceCurrency: normalizeCurrency(jld.Offers.PriceCur),
			Lat:           lat,
			Lon:           lon,
			Rooms:         rRooms,
			AreaTotal:     rAreaTotal,
			AreaLiving:    rAreaLiving,
			AreaKitchen:   rAreaKitchen,
			Floor:         rFloor,
			FloorsTotal:   rFloors,
			YearBuilt:     yearBuilt,
			HouseMaterial: houseMaterial,
			Metro:         metro,
			PricePeriod:   detectPricePeriod(cardDealType, jld, doc),
			Payload: map[string]any{
				"jsonld_raw": jld,
			},
		}
		log.Info("extracted fields",
			zap.String("id", externalID),
			zap.String("deal", cardDealType),
			zap.String("title", item.Title),
			zap.String("address", item.AddressText),
			zap.String("metro", metro),
			zap.Any("rooms", rRooms),
			zap.Any("area_total", rAreaTotal),
			zap.Any("floor", rFloor),
			zap.Any("floors_total", rFloors),
			zap.Any("year_built", yearBuilt),
			zap.String("material", houseMaterial),
		)
		if jld.Offers.Price != "" {
			if f, err := jld.Offers.Price.Float64(); err == nil {
				item.PriceValue = &f
			}
		}

		inserted, err := o.DBRepo.UpsertListingRaw(context.Background(), item)
		if err != nil {
			log.Warn("upsert raw", zap.Error(err), zap.String("url", pageURL))
			return
		}
		if inserted {
			if atomic.AddInt64(&upserted, 1)%50 == 0 {
				log.Info("upserted batch", zap.Int64("count", atomic.LoadInt64(&upserted)))
			}
			log.Info("upserted (insert)", zap.String("id", externalID), zap.String("url", pageURL))
			if o.MaxItems > 0 && atomic.LoadInt64(&upserted) >= int64(o.MaxItems) {
				if atomic.CompareAndSwapInt64(&shouldStop, 0, 1) {
					stopReason.Store("max_items")
				}
			}
		} else {
			log.Info("upserted (update)", zap.String("id", externalID), zap.String("url", pageURL))
		}
	})

	// 3) Контроль пагинации и пустых страниц
	if strings.TrimSpace(o.SingleURL) == "" {
		c.OnScraped(func(r *colly.Response) {
			// Нужен триггер только для страниц выдачи
			if detailRe.MatchString(r.Request.URL.Path) {
				return
			}
			pagesVisited++
			mu.Lock()
			linksFound := listingLinkCount[r.Request.URL.String()]
			mu.Unlock()
			if linksFound == 0 {
				emptyStreak++
			} else {
				emptyStreak = 0
			}
			log.Info("serp page done",
				zap.String("url", r.Request.URL.String()),
				zap.Int("pages_visited", pagesVisited),
				zap.Int("links_found", linksFound),
				zap.Int("empty_streak", emptyStreak),
				zap.Int64("upserted", atomic.LoadInt64(&upserted)),
			)
			if o.MaxEmptyListingPages > 0 && emptyStreak >= o.MaxEmptyListingPages {
				if atomic.CompareAndSwapInt64(&shouldStop, 0, 1) {
					stopReason.Store("empty_listing_pages")
				}
				return
			}
			// Следующая страница
			if pagesVisited < o.Pages && atomic.LoadInt64(&shouldStop) == 0 {
				currentPage++
				next := o.buildPageURL(currentPage)
				atomic.AddInt64(&inFlight, 1)
				log.Info("visit next page", zap.String("url", next))
				_ = c.Visit(next)
			} else if atomic.CompareAndSwapInt64(&shouldStop, 0, 1) {
				stopReason.Store("pages_limit")
			}
		})
	}

	c.OnRequest(func(r *colly.Request) {
		if o.MaxItems > 0 && atomic.LoadInt64(&upserted) >= int64(o.MaxItems) {
			if atomic.CompareAndSwapInt64(&shouldStop, 0, 1) {
				stopReason.Store("max_items")
			}
			r.Abort()
			return
		}
		// Блокируем любые PDF/экспорт-запросы
		if strings.Contains(r.URL.String(), "/export/pdf/") || strings.Contains(r.URL.String(), "/pdf/") {
			r.Abort()
			return
		}
		atomic.AddInt64(&inFlight, 1)
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		r.Headers.Set("Accept-Language", "ru,en;q=0.8")
		r.Headers.Set("Cache-Control", "no-cache")
		if strings.TrimSpace(o.Cookie) != "" {
			r.Headers.Set("Cookie", o.Cookie)
		}
		log.Info("http request", zap.String("url", r.URL.String()))
	})

	c.OnError(func(r *colly.Response, err error) {
		// Если был инкремент в OnRequest — компенсируем
		atomic.AddInt64(&inFlight, -1)
		log.Warn("http error", zap.Error(err), zap.Int("code", r.StatusCode), zap.String("url", r.Request.URL.String()))
	})

	if strings.TrimSpace(o.SingleURL) != "" {
		atomic.AddInt64(&inFlight, 1)
		log.Info("visit single url", zap.String("url", o.SingleURL))
		_ = c.Visit(o.SingleURL)
	} else {
		start := o.buildPageURL(currentPage)
		atomic.AddInt64(&inFlight, 1)
		log.Info("visit start page", zap.String("url", start))
		_ = c.Visit(start)
	}

	c.Wait()

	reason, _ := stopReason.Load().(string)
	log.Info("cian scrape finished",
		zap.Int64("upserted", atomic.LoadInt64(&upserted)),
		zap.Int("pages_visited", pagesVisited),
		zap.Int("empty_streak_end", emptyStreak),
		zap.Int64("in_flight_end", atomic.LoadInt64(&inFlight)),
		zap.String("stop_reason", reason),
		zap.Duration("elapsed", time.Since(startTS)),
	)
	return nil
}

func clampDuration(v, min, max time.Duration) time.Duration {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func extractIDFromURL(u string) string {
	// ЦИАН id обычно последняя цифробуквенная последовательность
	re := regexp.MustCompile(`(\d{6,})`)
	if m := re.FindStringSubmatch(u); len(m) >= 2 {
		return m[1]
	}
	// fallback — нормализованный URL
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.Trim(u, "/")
	u = strings.ReplaceAll(u, "/", "-")
	u = strings.Trim(u, "-")
	return u
}

func detectPricePeriod(dealType string, jld JSONLD, doc *goquery.Document) *string {
	if dealType == "sale" {
		return nil
	}
	text := strings.ToLower(jld.Description + " " + jld.Name + " " + doc.Text())
	if strings.Contains(text, "в месяц") || strings.Contains(text, "месяц") {
		v := "month"
		return &v
	}
	if strings.Contains(text, "сутки") || strings.Contains(text, "за сутки") {
		v := "day"
		return &v
	}
	return nil
}
