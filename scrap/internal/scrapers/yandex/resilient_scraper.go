package yandex

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
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

// ResilientOptions расширяет обычные Options дополнительными параметрами для надежности
type ResilientOptions struct {
	Options
	// Параметры устойчивости
	MaxRetries           int           // максимальное количество попыток для каждого URL
	BaseRetryDelay       time.Duration // базовая задержка для exponential backoff
	MaxRetryDelay        time.Duration // максимальная задержка между попытками
	CaptchaCooldown      time.Duration // пауза после обнаружения капчи
	ErrorThreshold       int           // количество ошибок подряд для активации защитного режима
	ProtectiveModeDelay  time.Duration // задержка в защитном режиме
	SessionRecoveryDelay time.Duration // пауза для восстановления сессии
	UserAgentRotation    bool          // ротация User-Agent
	ProxyRotation        []string      // список прокси для ротации
	EnableProgressSave   bool          // сохранение прогресса для восстановления
	ProgressFile         string        // файл для сохранения прогресса
}

// RetryState отслеживает состояние попыток для URL
type RetryState struct {
	Attempts    int
	LastAttempt time.Time
	LastError   string
}

// ScrapingSession отслеживает состояние всей сессии скрапинга
type ScrapingSession struct {
	StartTime          time.Time
	TotalRequests      int64
	SuccessfulRequests int64
	FailedRequests     int64
	CaptchaEncounters  int64
	ConsecutiveErrors  int64
	IsProtectiveMode   bool
	LastCaptcha        time.Time
}

// ResilientScraper обеспечивает надежный скрапинг с восстановлением после ошибок
type ResilientScraper struct {
	opts            ResilientOptions
	log             *zap.Logger
	session         *ScrapingSession
	retryStates     map[string]*RetryState
	userAgents      []string
	currentProxyIdx int
	mu              sync.RWMutex
}

func NewResilientScraper(opts ResilientOptions, log *zap.Logger) *ResilientScraper {
	// Значения по умолчанию для resilient параметров
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 5
	}
	if opts.BaseRetryDelay == 0 {
		opts.BaseRetryDelay = 2 * time.Second
	}
	if opts.MaxRetryDelay == 0 {
		opts.MaxRetryDelay = 5 * time.Minute
	}
	if opts.CaptchaCooldown == 0 {
		opts.CaptchaCooldown = 10 * time.Minute
	}
	if opts.ErrorThreshold == 0 {
		opts.ErrorThreshold = 10
	}
	if opts.ProtectiveModeDelay == 0 {
		opts.ProtectiveModeDelay = 30 * time.Second
	}
	if opts.SessionRecoveryDelay == 0 {
		opts.SessionRecoveryDelay = 2 * time.Minute
	}

	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
	}

	return &ResilientScraper{
		opts: opts,
		log:  log,
		session: &ScrapingSession{
			StartTime: time.Now(),
		},
		retryStates: make(map[string]*RetryState),
		userAgents:  userAgents,
	}
}

// calculateBackoffDelay вычисляет задержку с экспоненциальным откатом и jitter
func (rs *ResilientScraper) calculateBackoffDelay(attempt int) time.Duration {
	baseMs := rs.opts.BaseRetryDelay.Milliseconds()
	backoffMs := baseMs * int64(math.Pow(2, float64(attempt)))

	// Добавляем случайный jitter ±25%
	jitterRange := backoffMs / 4
	jitter, _ := rand.Int(rand.Reader, big.NewInt(jitterRange*2))
	backoffMs = backoffMs - jitterRange + jitter.Int64()

	delay := time.Duration(backoffMs) * time.Millisecond
	if delay > rs.opts.MaxRetryDelay {
		delay = rs.opts.MaxRetryDelay
	}

	return delay
}

// getRandomUserAgent возвращает случайный User-Agent
func (rs *ResilientScraper) getRandomUserAgent() string {
	if !rs.opts.UserAgentRotation || len(rs.userAgents) == 0 {
		return ""
	}

	idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(rs.userAgents))))
	return rs.userAgents[idx.Int64()]
}

// getNextProxy возвращает следующий прокси для ротации
func (rs *ResilientScraper) getNextProxy() string {
	if len(rs.opts.ProxyRotation) == 0 {
		return rs.opts.ProxyURL
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()

	proxy := rs.opts.ProxyRotation[rs.currentProxyIdx]
	rs.currentProxyIdx = (rs.currentProxyIdx + 1) % len(rs.opts.ProxyRotation)

	return proxy
}

// shouldRetry определяет, стоит ли повторить запрос
func (rs *ResilientScraper) shouldRetry(urlStr string, statusCode int, err error) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	state, exists := rs.retryStates[urlStr]
	if !exists {
		state = &RetryState{}
		rs.retryStates[urlStr] = state
	}

	// Проверяем лимит попыток
	if state.Attempts >= rs.opts.MaxRetries {
		return false
	}

	// Определяем, стоит ли повторить на основе кода ошибки
	switch statusCode {
	case 429, 503, 502, 504: // Rate limit, service unavailable, bad gateway, timeout
		return true
	case 403: // Forbidden - может быть блокировка
		return true
	case 0: // Network error
		return err != nil
	default:
		return false
	}
}

// markRetryAttempt отмечает попытку повтора
func (rs *ResilientScraper) markRetryAttempt(urlStr string, errorMsg string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	state, exists := rs.retryStates[urlStr]
	if !exists {
		state = &RetryState{}
		rs.retryStates[urlStr] = state
	}

	state.Attempts++
	state.LastAttempt = time.Now()
	state.LastError = errorMsg
}

// enterProtectiveMode активирует защитный режим при многих ошибках
func (rs *ResilientScraper) enterProtectiveMode() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.session.IsProtectiveMode {
		rs.session.IsProtectiveMode = true
		rs.log.Warn("entering protective mode due to consecutive errors",
			zap.Int64("consecutive_errors", rs.session.ConsecutiveErrors),
			zap.Duration("protective_delay", rs.opts.ProtectiveModeDelay))
	}
}

// exitProtectiveMode выходит из защитного режима
func (rs *ResilientScraper) exitProtectiveMode() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.session.IsProtectiveMode {
		rs.session.IsProtectiveMode = false
		rs.session.ConsecutiveErrors = 0
		rs.log.Info("exiting protective mode - requests successful again")
	}
}

// setupCollector создает и настраивает Colly collector с resilient параметрами
func (rs *ResilientScraper) setupCollector() *colly.Collector {
	c := colly.NewCollector(
		colly.AllowedDomains("realty.yandex.ru", "www.realty.yandex.ru", "yandex.ru"),
		colly.Async(true),
		colly.MaxDepth(2),
	)

	// Адаптивный таймаут
	timeout := rs.opts.DelayMax + 30*time.Second
	if rs.session.IsProtectiveMode {
		timeout *= 2
	}
	c.SetRequestTimeout(timeout)

	// User-Agent ротация
	if rs.opts.UserAgentRotation {
		c.UserAgent = rs.getRandomUserAgent()
	} else {
		extensions.RandomUserAgent(c)
	}

	if rs.opts.UseReferer {
		extensions.Referer(c)
	}

	// Адаптивные лимиты
	delay := rs.opts.DelayMin
	if rs.session.IsProtectiveMode {
		delay = rs.opts.ProtectiveModeDelay
	}

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: max(1, rs.opts.Parallelism),
		RandomDelay: rs.opts.DelayMax,
		Delay:       delay,
	})

	// Прокси ротация
	if len(rs.opts.ProxyRotation) > 0 {
		proxyURL := rs.getNextProxy()
		if proxyURL != "" {
			if pf, err := proxy.RoundRobinProxySwitcher(proxyURL); err == nil {
				c.SetProxyFunc(pf)
			}
		}
	} else if rs.opts.ProxyURL != "" {
		if pf, err := proxy.RoundRobinProxySwitcher(rs.opts.ProxyURL); err == nil {
			c.SetProxyFunc(pf)
		}
	}

	return c
}

// RunResilient запускает устойчивый к ошибкам скрапинг
func (rs *ResilientScraper) RunResilient() error {
	rs.log.Info("starting resilient scraping",
		zap.String("city", rs.opts.City),
		zap.String("deal", rs.opts.DealType),
		zap.Int("max_retries", rs.opts.MaxRetries),
		zap.Duration("base_retry_delay", rs.opts.BaseRetryDelay),
		zap.Duration("captcha_cooldown", rs.opts.CaptchaCooldown),
		zap.Int("error_threshold", rs.opts.ErrorThreshold))

	var upserted int64
	var processedDetails int64
	var shouldStop int64
	var stopReason atomic.Value
	var inFlight int64
	var mu sync.Mutex

	listingLinkCount := make(map[string]int)
	currentPage := rs.opts.StartPage
	pagesVisited := 0
	emptyStreak := 0

	for {
		// Проверка на капчу cooldown
		if time.Since(rs.session.LastCaptcha) < rs.opts.CaptchaCooldown {
			remaining := rs.opts.CaptchaCooldown - time.Since(rs.session.LastCaptcha)
			rs.log.Info("waiting for captcha cooldown", zap.Duration("remaining", remaining))
			time.Sleep(remaining)
		}

		// Защитный режим
		if rs.session.IsProtectiveMode {
			rs.log.Info("protective mode active, slowing down requests")
			time.Sleep(rs.opts.ProtectiveModeDelay)
		}

		c := rs.setupCollector()

		// Обработчик ссылок на детали
		if strings.TrimSpace(rs.opts.SingleURL) == "" {
			c.OnHTML("a[href]", func(e *colly.HTMLElement) {
				href := e.Attr("href")

				if href == "" || !detailRe.MatchString(href) {
					return
				}
				if detailRe.MatchString(e.Request.URL.Path) {
					return
				}
				if atomic.LoadInt64(&shouldStop) == 1 {
					return
				}

				link := e.Request.AbsoluteURL(href)
				mu.Lock()
				listingLinkCount[e.Request.URL.String()]++
				mu.Unlock()

				// Запланировать обход детали с retry logic
				rs.scheduleDetailVisit(c, link)
			})
		} // Обработчик ответов
		c.OnResponse(func(r *colly.Response) {
			defer atomic.AddInt64(&inFlight, -1)
			atomic.AddInt64(&rs.session.TotalRequests, 1)

			// Обнаружение капчи
			if strings.Contains(r.Request.URL.Host, "yandex.") && strings.Contains(r.Request.URL.Path, "showcaptcha") {
				atomic.AddInt64(&rs.session.CaptchaEncounters, 1)
				rs.session.LastCaptcha = time.Now()

				rs.log.Warn("captcha detected - entering extended cooldown",
					zap.String("url", r.Request.URL.String()),
					zap.Duration("cooldown", rs.opts.CaptchaCooldown))

				atomic.StoreInt64(&shouldStop, 1)
				stopReason.Store("captcha")
				return
			}

			// Сброс счетчика последовательных ошибок при успешном ответе
			if atomic.LoadInt64(&rs.session.ConsecutiveErrors) > 0 {
				atomic.StoreInt64(&rs.session.ConsecutiveErrors, 0)
				rs.exitProtectiveMode()
			}

			atomic.AddInt64(&rs.session.SuccessfulRequests, 1)

			// Парсинг детальной страницы
			if detailRe.MatchString(r.Request.URL.Path) {
				rs.processDetailPage(r, &upserted, &processedDetails)
			}

			// Проверка лимита
			if rs.opts.MaxItems > 0 && atomic.LoadInt64(&upserted) >= int64(rs.opts.MaxItems) {
				rs.log.Info("max items reached, stopping", zap.Int("max_items", rs.opts.MaxItems))
				atomic.StoreInt64(&shouldStop, 1)
				stopReason.Store("max_items")
			}
		})

		// Обработчик ошибок с retry logic
		c.OnError(func(r *colly.Response, err error) {
			if atomic.LoadInt64(&inFlight) > 0 {
				atomic.AddInt64(&inFlight, -1)
			}

			atomic.AddInt64(&rs.session.FailedRequests, 1)
			atomic.AddInt64(&rs.session.ConsecutiveErrors, 1)

			urlStr := ""
			statusCode := 0
			if r != nil && r.Request != nil {
				urlStr = r.Request.URL.String()
				statusCode = r.StatusCode
			}

			// Активация защитного режима при многих ошибках подряд
			if rs.session.ConsecutiveErrors >= int64(rs.opts.ErrorThreshold) {
				rs.enterProtectiveMode()
			}

			rs.log.Warn("request error",
				zap.String("url", urlStr),
				zap.Int("status", statusCode),
				zap.Error(err),
				zap.Int64("consecutive_errors", rs.session.ConsecutiveErrors))

			// Попытка повтора
			if urlStr != "" && rs.shouldRetry(urlStr, statusCode, err) {
				rs.scheduleRetry(c, urlStr, statusCode, err)
			}
		})

		c.OnRequest(func(r *colly.Request) {
			if atomic.LoadInt64(&shouldStop) == 1 {
				r.Abort()
				return
			}

			atomic.AddInt64(&inFlight, 1)

			// Динамическая установка заголовков
			r.Headers.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
			r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
			// Внимаем: встроенная декомпрессия Colly поддерживает gzip/deflate; br может вызвать проблемы без brotli
			r.Headers.Set("Accept-Encoding", "gzip, deflate")
			r.Headers.Set("DNT", "1")
			r.Headers.Set("Upgrade-Insecure-Requests", "1")

			if rs.opts.Cookie != "" {
				r.Headers.Set("Cookie", rs.opts.Cookie)
			}

			// Ротация User-Agent для каждого запроса
			if rs.opts.UserAgentRotation {
				if ua := rs.getRandomUserAgent(); ua != "" {
					r.Headers.Set("User-Agent", ua)
				}
			}
		})

		// После полного парсинга страницы выдачи логируем, сколько детальных ссылок было найдено
		c.OnScraped(func(r *colly.Response) {
			urlStr := r.Request.URL.String()
			// только для страниц выдачи
			if detailRe.MatchString(r.Request.URL.Path) {
				return
			}
			mu.Lock()
			cnt := listingLinkCount[urlStr]
			mu.Unlock()
			if cnt > 0 {
				// сбросить счётчик пустых страниц
				if emptyStreak > 0 {
					emptyStreak = 0
				}
				rs.log.Info("listing page parsed",
					zap.String("url", urlStr),
					zap.Int("detail_links", cnt),
					zap.Int("pages_visited_next", pagesVisited+1),
					zap.Int64("new_items_so_far", atomic.LoadInt64(&upserted)),
					zap.Int64("total_processed_details", atomic.LoadInt64(&processedDetails)))
			} else {
				emptyStreak++
				rs.log.Info("listing page has no detail links",
					zap.String("url", urlStr),
					zap.Int("empty_streak", emptyStreak),
					zap.Int64("new_items_so_far", atomic.LoadInt64(&upserted)),
					zap.Int64("total_processed_details", atomic.LoadInt64(&processedDetails)))
			}
		})

		// Запуск сбора
		rs.log.Info("running scraping cycle", zap.Int("current_page", currentPage), zap.Int("pages_visited", pagesVisited))
		err := rs.runScrapingCycle(c, &currentPage, &pagesVisited, &emptyStreak, &shouldStop, &stopReason, &listingLinkCount)

		rs.log.Info("waiting for collector to finish")
		c.Wait()
		rs.log.Info("collector finished")

		// Проверка условий завершения
		if atomic.LoadInt64(&shouldStop) == 1 {
			reason, _ := stopReason.Load().(string)
			rs.logFinalStats(upserted, reason)

			// Если остановились из-за капчи, ждем и попробуем еще раз
			if reason == "captcha" && atomic.LoadInt64(&upserted) < int64(rs.opts.MaxItems) {
				rs.log.Info("captcha cooldown completed, attempting to resume",
					zap.Duration("cooldown", rs.opts.CaptchaCooldown))

				// Сброс состояния для продолжения
				atomic.StoreInt64(&shouldStop, 0)
				stopReason.Store("")

				// Дополнительная пауза для восстановления
				time.Sleep(rs.opts.SessionRecoveryDelay)
				continue
			}

			break
		}

		if err != nil {
			rs.log.Error("scraping cycle error", zap.Error(err))
			// Пауза перед повтором цикла
			time.Sleep(rs.opts.SessionRecoveryDelay)
			continue
		}

		// Если достигли максимума элементов или страниц - завершаем
		if rs.opts.MaxItems > 0 && atomic.LoadInt64(&upserted) >= int64(rs.opts.MaxItems) {
			break
		}

		if rs.opts.Pages > 0 && pagesVisited >= rs.opts.Pages {
			break
		}

		// Пауза между циклами для стабильности
		time.Sleep(2 * time.Second)
	}

	return nil
}

// scheduleDetailVisit планирует посещение детальной страницы
func (rs *ResilientScraper) scheduleDetailVisit(c *colly.Collector, url string) {
	if err := c.Visit(url); err != nil {
		// Игнорируем "already visited" ошибки как нормальные
	}
}

// scheduleRetry планирует повторную попытку с backoff
func (rs *ResilientScraper) scheduleRetry(c *colly.Collector, urlStr string, statusCode int, err error) {
	rs.markRetryAttempt(urlStr, fmt.Sprintf("HTTP %d: %v", statusCode, err))

	rs.mu.RLock()
	state := rs.retryStates[urlStr]
	attempts := state.Attempts
	rs.mu.RUnlock()

	delay := rs.calculateBackoffDelay(attempts)

	rs.log.Info("scheduling retry",
		zap.String("url", urlStr),
		zap.Int("attempt", attempts),
		zap.Duration("delay", delay))

	go func() {
		time.Sleep(delay)
		if err := c.Visit(urlStr); err != nil {
			// Игнорируем ошибки retry как нормальные
		}
	}()
}

// processDetailPage обрабатывает детальную страницу объявления
func (rs *ResilientScraper) processDetailPage(r *colly.Response, upserted *int64, processedDetails *int64) {
	if rs.opts.MaxItems > 0 && atomic.LoadInt64(upserted) >= int64(rs.opts.MaxItems) {
		return
	}

	// Учитываем любую детальную страницу (вставка или обновление)
	if processedDetails != nil {
		atomic.AddInt64(processedDetails, 1)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(r.Body))
	if err != nil {
		rs.log.Warn("goquery parsing error", zap.Error(err))
		return
	}

	// Парсинг данных (используем существующие функции)
	jld, _ := parseJSONLD(doc)

	// Сборка адреса и координат с фолбэками
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

	// Фолбэки для адреса и координат
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

	addrText = strings.TrimSpace(strings.Trim(addrText, ", "))

	// Извлечение характеристик
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
		DealType:      rs.opts.DealType,
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
		PricePeriod:   detectPricePeriod(rs.opts.DealType, jld, doc),
		Payload: map[string]any{
			"jsonld_raw": jld,
		},
	}

	if jld.Offers.Price != "" {
		if f := parseFloat(string(jld.Offers.Price)); f != nil {
			item.PriceValue = f
		}
	}

	inserted, err := rs.opts.DBRepo.UpsertListingRaw(context.Background(), item)
	if err != nil {
		rs.log.Warn("database upsert error", zap.Error(err), zap.String("ext_id", item.ExternalID))
		return
	}

	if inserted {
		newCount := atomic.AddInt64(upserted, 1)
		rs.log.Info("inserted new listing",
			zap.String("ext_id", item.ExternalID),
			zap.Int64("total_new", newCount))
	} else {
		rs.log.Debug("updated existing listing", zap.String("ext_id", item.ExternalID))
	}
}

// runScrapingCycle выполняет один цикл скрапинга
func (rs *ResilientScraper) runScrapingCycle(c *colly.Collector, currentPage, pagesVisited, emptyStreak *int, shouldStop *int64, stopReason *atomic.Value, listingLinkCount *map[string]int) error {
	if strings.TrimSpace(rs.opts.SingleURL) != "" {
		rs.log.Info("visiting single URL", zap.String("url", rs.opts.SingleURL))
		return c.Visit(rs.opts.SingleURL)
	}

	rs.log.Info("starting scraping cycle",
		zap.Int("start_page", rs.opts.StartPage),
		zap.Int("pages", rs.opts.Pages),
		zap.Int("current_page", *currentPage))

	// Планируем обход страниц выдачи
	for page := *currentPage; page < rs.opts.StartPage+rs.opts.Pages; page++ {
		if atomic.LoadInt64(shouldStop) == 1 {
			break
		}

		pageURL := rs.opts.Options.buildPageURL(page, rs.log)
		rs.log.Info("visiting listing page", zap.String("url", pageURL), zap.Int("page", page))

		if err := c.Visit(pageURL); err != nil {
			rs.log.Warn("failed to visit listing page", zap.String("url", pageURL), zap.Error(err))
			continue
		}

		*currentPage = page
		*pagesVisited++

		// Контролируем пагинацию
		if rs.opts.MaxEmptyListingPages > 0 && *emptyStreak >= rs.opts.MaxEmptyListingPages {
			rs.log.Info("stopping due to empty pages streak", zap.Int("streak", *emptyStreak))
			atomic.StoreInt64(shouldStop, 1)
			stopReason.Store("empty_listing_pages")
			break
		}
	}

	return nil
}

// logFinalStats выводит финальную статистику
func (rs *ResilientScraper) logFinalStats(upserted int64, reason string) {
	elapsed := time.Since(rs.session.StartTime)

	rs.log.Info("scraping session completed",
		zap.Int64("new_items", upserted),
		zap.Int64("total_requests", rs.session.TotalRequests),
		zap.Int64("successful_requests", rs.session.SuccessfulRequests),
		zap.Int64("failed_requests", rs.session.FailedRequests),
		zap.Int64("captcha_encounters", rs.session.CaptchaEncounters),
		zap.String("stop_reason", reason),
		zap.Duration("elapsed", elapsed),
		zap.Float64("success_rate", float64(rs.session.SuccessfulRequests)/float64(rs.session.TotalRequests)*100))
}

// RunWithResilientScraper является wrapper функцией для обратной совместимости
func RunWithResilientScraper(o Options, log *zap.Logger) error {
	resilientOpts := ResilientOptions{
		Options:              o,
		MaxRetries:           5,
		BaseRetryDelay:       2 * time.Second,
		MaxRetryDelay:        5 * time.Minute,
		CaptchaCooldown:      10 * time.Minute,
		ErrorThreshold:       10,
		ProtectiveModeDelay:  30 * time.Second,
		SessionRecoveryDelay: 2 * time.Minute,
		UserAgentRotation:    true,
		ProxyRotation:        []string{}, // можно добавить список прокси
	}

	scraper := NewResilientScraper(resilientOpts, log)
	return scraper.RunResilient()
}
