package yandex

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

	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/extensions"
	"github.com/gocolly/colly/v2/proxy"
	"go.uber.org/zap"

	"github.com/PuerkitoBio/goquery"
)

type Options struct {
	DBRepo       *repo.RawRepository
	StartURLTmpl string // например: https://realty.yandex.ru/{city}/kupit/kvartira/?page={page}
	City         string
	StartPage    int
	Pages        int
	SnapshotDir  string
	ProxyURL     string // опционально
	DelayMin     time.Duration
	DelayMax     time.Duration
	Parallelism  int
	// Доп. маскировка
	Cookie     string // опционально: строка Cookie из браузера для realty.yandex.ru
	UseReferer bool   // включить автоматический Referer
	// Новые параметры управления завершением
	MaxItems             int    // 0 = без лимита; при достижении — остановка скрапинга
	MaxEmptyListingPages int    // 0 = игнорировать; >0 — остановить после N подряд страниц выдачи без ссылок на детали
	DealType             string // "sale" | "rent_long" | "rent_daily"
}

var detailRe = regexp.MustCompile(`/offer/\d+/`)

func (o Options) buildPageURL(page int, log *zap.Logger) string {
	u := strings.ReplaceAll(o.StartURLTmpl, "{city}", url.PathEscape(o.City))
	u = strings.ReplaceAll(u, "{page}", fmt.Sprintf("%d", page))
	return u
}

func Run(o Options, log *zap.Logger) error {
	startTS := time.Now()
	// Снапшоты HTML более не сохраняем

	c := colly.NewCollector(
		colly.AllowedDomains("realty.yandex.ru", "www.realty.yandex.ru", "yandex.ru"),
		colly.Async(true),
		colly.MaxDepth(2),
	)
	// Жёсткий таймаут на HTTP-запросы, чтобы аборты/зависшие коннекты быстро завершались
	c.SetRequestTimeout(clampDuration(15*time.Second, 5*time.Second, 60*time.Second))
	extensions.RandomUserAgent(c)
	if o.UseReferer {
		extensions.Referer(c)
	}
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: max(1, o.Parallelism),
		RandomDelay: clampDuration(o.DelayMax, 0, time.Second*10),
		Delay:       clampDuration(o.DelayMin, 0, time.Second*5),
	})

	if o.ProxyURL != "" {
		pf, err := proxy.RoundRobinProxySwitcher(o.ProxyURL)
		if err != nil {
			log.Warn("proxy setup error: ", zap.Error(err))
		} else {
			c.SetProxyFunc(pf)
		}
	}

	// Счётчики/состояние
	var upserted int64 // успешно сохраненные карточки
	var shouldStop int64
	var stopReason atomic.Value // string: "max_items" | "empty_listing_pages" | "pages_limit" | "manual"
	var inFlight int64          // активные HTTP-запросы (для graceful shutdown наблюдения)
	var mu sync.Mutex
	listingLinkCount := make(map[string]int) // URL страницы выдачи -> кол-во детальных ссылок
	currentPage := o.StartPage
	pagesVisited := 0
	emptyStreak := 0

	// 1) на страницах выдачи собираем ссылки на детали и считаем их
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		if href == "" || !detailRe.MatchString(href) {
			return
		}
		// учитываем только если это страница выдачи (не детальная)
		if detailRe.MatchString(e.Request.URL.Path) {
			return
		}
		// если уже решили останавливаться — не планируем деталей
		if atomic.LoadInt64(&shouldStop) == 1 {
			return
		}
		link := e.Request.AbsoluteURL(href)
		// инкремент счётчика для текущей страницы выдачи
		mu.Lock()
		listingLinkCount[e.Request.URL.String()]++
		mu.Unlock()
		_ = c.Visit(link)
	})

	// 2) обработчик карточки
	c.OnResponse(func(r *colly.Response) {
		// уменьшаем in-flight при любом ответе (включая ранние возвраты)
		defer atomic.AddInt64(&inFlight, -1)
		// Обнаружение капчи: если редирект/ответ ведёт на showcaptcha — останавливаемся аккуратно
		if strings.Contains(r.Request.URL.Host, "yandex.") && strings.Contains(r.Request.URL.Path, "showcaptcha") {
			if atomic.LoadInt64(&shouldStop) == 0 {
				log.Info("captcha detected, graceful stop", zap.String("url", r.Request.URL.String()))
				atomic.StoreInt64(&shouldStop, 1)
				stopReason.Store("captcha")
			}
			return
		}
		// если достигли лимита — не тратим время на парсинг/запись
		if o.MaxItems > 0 && atomic.LoadInt64(&upserted) >= int64(o.MaxItems) {
			return
		}
		if !strings.Contains(r.Headers.Get("Content-Type"), "text/html") {
			return
		}
		// детальная страница?
		if !detailRe.MatchString(r.Request.URL.Path) {
			return
		}
		// снапшот HTML не сохраняем

		// парсим DOM
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(r.Body))
		if err != nil {
			log.Warn("goquery err: ", zap.Error(err))
			return
		}
		jld, _ := parseJSONLD(doc)

		// Фолбэк: если из JSON-LD не получили адрес или координаты, попробуем достать из inline JSON
		// Сформируем адрес из JSON-LD: отдаём street, затем locality, фильтруя пустые поля
		addrParts := make([]string, 0, 2)
		if s := strings.TrimSpace(jld.Address.StreetAddress); s != "" {
			addrParts = append(addrParts, s)
		}
		if s := strings.TrimSpace(jld.Address.AddressLocal); s != "" {
			addrParts = append(addrParts, s)
		}
		addrText := strings.Join(addrParts, ", ")
		lat := jld.Geo.Latitude
		lon := jld.Geo.Longitude
		// 2.1) Попробуем вытащить адрес/координаты из DOM (атрибуты data-latitude/longitude)
		if addrText == "" || lat == nil || lon == nil {
			if a2, flat, flon := extractAddressAndPointFromDoc(doc); a2 != "" || flat != nil || flon != nil {
				if strings.TrimSpace(addrText) == "" && a2 != "" {
					addrText = a2
				}
				if lat == nil && flat != nil {
					lat = flat
				}
				if lon == nil && flon != nil {
					lon = flon
				}
			}
		}
		// 2.2) Если не помогло — regex по inline JSON
		if addrText == "" || lat == nil || lon == nil {
			street, flat, flon := extractAddressAndPointFromHTML(string(r.Body))
			if strings.TrimSpace(addrText) == "" && street != "" {
				addrText = strings.TrimSpace(street)
			}
			if lat == nil && flat != nil {
				lat = flat
			}
			if lon == nil && flon != nil {
				lon = flon
			}
		}

		pageURL := r.Request.URL.String()
		externalID := extractIDFromURL(pageURL)

		// Финальная зачистка адреса: убрать висячие запятые и двойные пробелы
		addrText = strings.TrimSpace(strings.Trim(addrText, ", "))
		// Факты из текста и DOM (комнаты/площадь/этаж/метро)
		rRooms, rAreaTotal, rFloor, rFloors := extractFactsFromText(jld.Name, jld.Description)
		if rRooms == nil {
			if rr := extractRoomsFromDoc(doc); rr != nil {
				rRooms = rr
			}
		}
		metro := extractMetroFromDoc(doc)
		rAreaLiving, rAreaKitchen := extractAreasFromDoc(doc)
		yearBuilt, houseMaterial := extractHouseInfoFromDoc(doc)

		item := repo.RawItem{
			Source:        "yandex",
			DealType:      o.DealType,
			ExternalID:    externalID,
			URL:           pageURL,
			Title:         strings.TrimSpace(jld.Name),
			Description:   strings.TrimSpace(jld.Description),
			AddressText:   addrText,
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
			PricePeriod:   detectPricePeriod(o.DealType, jld, doc),
			Payload: map[string]any{
				"jsonld_raw": jld,
			},
		}
		if jld.Offers.Price != "" {
			if f := parseFloat(string(jld.Offers.Price)); f != nil {
				item.PriceValue = f
			}
		}

		if err := o.DBRepo.UpsertListingRaw(context.Background(), item); err != nil {
			log.Warn("upsert raw err: ", zap.Error(err))
		} else {
			msg := fmt.Sprintf("upserted %s %s", item.Source, item.ExternalID)
			log.Info(msg)
			// инкремент и проверка лимита
			if o.MaxItems > 0 {
				if atomic.AddInt64(&upserted, 1) >= int64(o.MaxItems) {
					log.Info("max items reached, initiating graceful stop (no new requests)", zap.Int("max_items", o.MaxItems))
					atomic.StoreInt64(&shouldStop, 1)
					stopReason.Store("max_items")
				}
			}
		}
	})

	// 3) по окончании обработки любой страницы решаем, продолжать ли пагинацию выдачи
	c.OnScraped(func(r *colly.Response) {
		// интересуют только страницы выдачи (не деталки)
		if detailRe.MatchString(r.Request.URL.Path) {
			return
		}
		// обновим streak пустых страниц
		mu.Lock()
		cnt := listingLinkCount[r.Request.URL.String()]
		mu.Unlock()
		// информативный лог по странице выдачи
		log.Info("list page scanned",
			zap.String("url", r.Request.URL.String()),
			zap.Int("detail_links", cnt),
			zap.Int("pages_visited_next", pagesVisited+1),
		)
		if cnt == 0 {
			emptyStreak++
		} else {
			emptyStreak = 0
		}
		pagesVisited++
		// условия остановки пагинации
		if o.MaxEmptyListingPages > 0 && emptyStreak >= o.MaxEmptyListingPages {
			log.Info("stop on empty listing pages (will not schedule more)", zap.Int("streak", emptyStreak))
			atomic.StoreInt64(&shouldStop, 1)
			stopReason.Store("empty_listing_pages")
			return
		}
		if atomic.LoadInt64(&shouldStop) == 1 {
			return
		}
		if o.Pages > 0 && pagesVisited >= max(1, o.Pages) {
			stopReason.Store("pages_limit")
			return // достигли план по страницам
		}
		// запустить следующую страницу
		next := currentPage + 1
		if next >= o.StartPage+max(1, o.Pages) {
			return
		}
		currentPage = next
		u := o.buildPageURL(currentPage, log)
		if err := c.Visit(u); err != nil {
			log.Warn("visit list err: ", zap.Error(err))
		}
	})

	c.OnRequest(func(r *colly.Request) {
		// если достигнут лимит — отменяем дальнейшие запросы
		if o.MaxItems > 0 && atomic.LoadInt64(&upserted) >= int64(o.MaxItems) {
			r.Abort()
			return
		}
		if atomic.LoadInt64(&shouldStop) == 1 {
			r.Abort()
			return
		}
		// учтём активный запрос только если он не был отменён
		atomic.AddInt64(&inFlight, 1)
		// базовые заголовки
		r.Headers.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
		if o.Cookie != "" {
			r.Headers.Set("Cookie", o.Cookie)
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		// уменьшаем in-flight при ошибочном ответе, если был инкремент
		if atomic.LoadInt64(&inFlight) > 0 {
			atomic.AddInt64(&inFlight, -1)
		}
		if r != nil && r.Request != nil {
			msg := fmt.Sprintf("HTTP %d %s: %v", r.StatusCode, r.Request.URL, err)
			log.Info(msg)
		} else {
			log.Info("request error", zap.Error(err))
		}
	})

	// запускам обход: стартуем с первой страницы, дальше листаем в OnScraped
	u := o.buildPageURL(currentPage, log)
	if err := c.Visit(u); err != nil {
		log.Warn("visit list err: ", zap.Error(err))
	}

	c.Wait()
	// Финальный сводный лог
	reason, _ := stopReason.Load().(string)
	log.Info("scrape finished",
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

func detectPricePeriod(dealType string, jld JSONLD, doc *goquery.Document) *string {
	if dealType == "sale" {
		return nil
	}
	// Поиск по описанию, заголовку, DOM
	text := strings.ToLower(jld.Description + " " + jld.Name + " " + doc.Text())
	if strings.Contains(text, "в месяц") || strings.Contains(text, "месяц") {
		s := "month"
		return &s
	}
	if strings.Contains(text, "сутки") || strings.Contains(text, "за сутки") {
		s := "day"
		return &s
	}
	return nil
}
