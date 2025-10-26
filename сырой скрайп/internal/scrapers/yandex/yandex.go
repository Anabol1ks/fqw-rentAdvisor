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
	SingleURL    string // если задан, обойти только этот URL карточки
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

// Детальная страница объявления ТОЛЬКО формата /offer/<id> (агрегаторы вида /st-... - игнорируем)
var detailRe = regexp.MustCompile(`/(?:offer/\d+)(?:[/?#]|$)`)

func (o Options) buildPageURL(page int, log *zap.Logger) string {
	u := strings.ReplaceAll(o.StartURLTmpl, "{city}", url.PathEscape(o.City))
	u = strings.ReplaceAll(u, "{page}", fmt.Sprintf("%d", page))
	return u
}

func Run(o Options, log *zap.Logger) error {
	// Используем resilient scraper для более надежного сбора данных
	return RunWithResilientScraper(o, log)
}

func RunLegacy(o Options, log *zap.Logger) error {
	startTS := time.Now()
	// Снапшоты HTML более не сохраняем

	c := colly.NewCollector(
		colly.AllowedDomains("realty.yandex.ru", "www.realty.yandex.ru", "yandex.ru"),
		colly.Async(true),
		colly.MaxDepth(2),
	)
	// Таймаут HTTP-запросов: адаптивно от пейсинга (как в cian)
	c.SetRequestTimeout(clampDuration(o.DelayMax+10*time.Second, 5*time.Second, 60*time.Second))
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

	// Стартовый информативный лог (диагностика)
	log.Info("yandex scrape start",
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

	// Счётчики/состояние
	var upserted int64  // успешно сохраненные НОВЫЕ карточки
	var processed int64 // все обработанные карточки (новые + обновленные)
	var shouldStop int64
	var stopReason atomic.Value // string: "max_items" | "empty_listing_pages" | "pages_limit" | "manual"
	var inFlight int64          // активные HTTP-запросы (для graceful shutdown наблюдения)
	var mu sync.Mutex
	listingLinkCount := make(map[string]int) // URL страницы выдачи -> кол-во детальных ссылок
	currentPage := o.StartPage
	pagesVisited := 0
	emptyStreak := 0
	// Простой ограниченный ретрай для страниц выдачи (по URL)
	retryCount := make(map[string]int)

	// 1) на страницах выдачи собираем ссылки на детали и считаем их (если не single URL режим)
	if strings.TrimSpace(o.SingleURL) == "" {
		// Для диагностики - считаем все элементы a на странице
		c.OnHTML("a", func(e *colly.HTMLElement) {
			// Считаем только первые 5 элементов для логирования
			mu.Lock()
			linkCount := len(listingLinkCount)
			mu.Unlock()
			if linkCount < 5 {
				log.Info("found a element",
					zap.String("href", e.Attr("href")),
					zap.String("text", strings.TrimSpace(e.Text)),
					zap.String("page_url", e.Request.URL.String()))
			}
		})

		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			href := e.Attr("href")
			if href == "" {
				return
			}

			// Отладочное логирование всех ссылок (первые 10)
			mu.Lock()
			linkCount := len(listingLinkCount)
			mu.Unlock()
			if linkCount < 10 && strings.Contains(href, "/") {
				log.Info("found link", zap.String("href", href), zap.String("page_url", e.Request.URL.String()), zap.Bool("matches_offer_regex", detailRe.MatchString(href)))
			}

			if !detailRe.MatchString(href) {
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

			log.Info("visiting detail page", zap.String("link", link))
			_ = c.Visit(link)
		})
	} // 2) обработчик карточки
	c.OnResponse(func(r *colly.Response) {
		// уменьшаем in-flight при любом ответе (включая ранние возвраты)
		defer atomic.AddInt64(&inFlight, -1)

		// Логируем все ответы для диагностики
		log.Debug("received response",
			zap.String("url", r.Request.URL.String()),
			zap.Int("status", r.StatusCode),
			zap.String("content_type", r.Headers.Get("Content-Type")),
			zap.Int("content_length", len(r.Body)),
			zap.Bool("is_detail_page", detailRe.MatchString(r.Request.URL.Path)))

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

		inserted, err := o.DBRepo.UpsertListingRaw(context.Background(), item)
		if err != nil {
			log.Warn("upsert raw err", zap.Error(err), zap.String("ext_id", item.ExternalID))
			return
		}

		// Увеличиваем счетчик обработанных записей в любом случае
		atomic.AddInt64(&processed, 1)

		if inserted {
			log.Info("inserted new", zap.String("ext_id", item.ExternalID))
			if o.MaxItems > 0 {
				if atomic.AddInt64(&upserted, 1) >= int64(o.MaxItems) {
					log.Info("max NEW items reached, initiating graceful stop", zap.Int("max_items", o.MaxItems))
					atomic.StoreInt64(&shouldStop, 1)
					stopReason.Store("max_items")
				}
			}
		} else {
			log.Debug("updated existing", zap.String("ext_id", item.ExternalID))
		}
	})

	// 3) по окончании обработки любой страницы решаем, продолжать ли пагинацию выдачи
	if strings.TrimSpace(o.SingleURL) == "" {
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
				zap.Int64("new_items_so_far", atomic.LoadInt64(&upserted)),
				zap.Int64("total_processed_so_far", atomic.LoadInt64(&processed)),
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
				// достигли последней страницы по лимиту — фиксируем причину
				stopReason.Store("pages_limit")
				return
			}
			currentPage = next
			u := o.buildPageURL(currentPage, log)
			if err := c.Visit(u); err != nil {
				log.Warn("visit list err: ", zap.Error(err))
			}
		})
	}

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
			// Если это ошибка на странице выдачи (а не детальной карточке) — не "глохнем" молча
			if strings.TrimSpace(o.SingleURL) == "" && !detailRe.MatchString(r.Request.URL.Path) {
				urlStr := r.Request.URL.String()
				// Попробуем 1 раз ретраить страницу выдачи
				mu.Lock()
				rc := retryCount[urlStr]
				if rc < 1 && atomic.LoadInt64(&shouldStop) == 0 {
					retryCount[urlStr] = rc + 1
					mu.Unlock()
					log.Info("retry list page once", zap.String("url", urlStr))
					// Повторный визит той же страницы
					_ = c.Visit(urlStr)
					return
				}
				mu.Unlock()
				// Считаем как пустую и двигаемся дальше по пагинации, чтобы не останавливаться
				emptyStreak++
				pagesVisited++
				log.Info("list page error treated as empty, continue",
					zap.String("url", urlStr),
					zap.Int("pages_visited", pagesVisited),
					zap.Int("empty_streak", emptyStreak),
				)
				if o.MaxEmptyListingPages > 0 && emptyStreak >= o.MaxEmptyListingPages {
					atomic.StoreInt64(&shouldStop, 1)
					stopReason.Store("empty_listing_pages")
					return
				}
				if o.Pages > 0 && pagesVisited >= max(1, o.Pages) {
					stopReason.Store("pages_limit")
					return
				}
				if atomic.LoadInt64(&shouldStop) == 1 {
					return
				}
				// Планируем следующую, если не вышли за предел
				next := currentPage + 1
				if next < o.StartPage+max(1, o.Pages) {
					currentPage = next
					u := o.buildPageURL(currentPage, log)
					if err := c.Visit(u); err != nil {
						log.Warn("visit list err: ", zap.Error(err))
					}
				} else {
					// Если сюда попали, то это фактически лимит по страницам
					stopReason.Store("pages_limit")
				}
				return
			}
		} else {
			log.Info("request error", zap.Error(err))
		}
	})

	// запускам обход: либо одиночный URL, либо стартовая страница выдачи
	if strings.TrimSpace(o.SingleURL) != "" {
		if err := c.Visit(o.SingleURL); err != nil {
			log.Warn("visit single url err: ", zap.Error(err), zap.String("url", o.SingleURL))
		}
	} else {
		u := o.buildPageURL(currentPage, log)
		if err := c.Visit(u); err != nil {
			log.Warn("visit list err: ", zap.Error(err))
		}
	}

	c.Wait()
	// Финальный сводный лог
	reason, _ := stopReason.Load().(string)
	if strings.TrimSpace(reason) == "" {
		reason = "drained" // очередь запросов исчерпана без явного условия остановки
	}
	log.Info("scrape finished",
		zap.Int64("new_items", atomic.LoadInt64(&upserted)),
		zap.Int64("total_processed", atomic.LoadInt64(&processed)),
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
