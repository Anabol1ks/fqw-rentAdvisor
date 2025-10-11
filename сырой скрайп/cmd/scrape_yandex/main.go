package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"skripe/config"
	"skripe/internal/repo"
	"skripe/internal/scrapers/yandex"
	"skripe/internal/storage"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/Anabol1ks/rentAdvisor-utils-go/logger"
)

func main() {
	_ = godotenv.Load()                 // Загружаем стандартный .env
	_ = godotenv.Load(".env.resilient") // Загружаем resilient настройки
	isDev := true
	if err := logger.Init(isDev); err != nil {
		panic(err)
	}

	defer logger.Sync()

	log := logger.L()

	cfg := config.Load(log)

	db := storage.ConnectDB(&cfg.DB, log)
	var (
		city       = flag.String("city", "moskva", "Город в URL (пример: moskva, peterburg)")
		startPage  = flag.Int("start", 1, "Стартовая страница")
		pages      = flag.Int("pages", 1, "Сколько страниц обойти")
		urlOnly    = flag.String("url", "", "Тест: конкретный URL карточки (обходит только его)")
		proxyURL   = flag.String("proxy", "", "HTTP/HTTPS proxy (опционально)")
		delayMin   = flag.Duration("delay-min", 1200*time.Millisecond, "Минимальная задержка между запросами")
		delayMax   = flag.Duration("delay-max", 2500*time.Millisecond, "Максимальная задержка между запросами")
		parallel   = flag.Int("parallel", 2, "Параллелизм")
		maxItems   = flag.Int("max-items", 0, "Максимум карточек для сбора (0 = без лимита)")
		maxEmpty   = flag.Int("max-empty-pages", 0, "Остановить после N подряд пустых страниц выдачи (0 = игнорировать)")
		deal       = flag.String("deal", "sale", "sale|rent")
		cookieFlag = flag.String("cookie", "", "Строка Cookie для realty.yandex.ru (опционально)")
		useRef     = flag.Bool("use-referer", true, "Включить заголовок Referer для Colly")

		// Новые параметры для resilient scraper
		maxRetries        = flag.Int("max-retries", 5, "Максимальное количество попыток для каждого URL")
		baseRetryDelay    = flag.Duration("base-retry-delay", 2*time.Second, "Базовая задержка для exponential backoff")
		maxRetryDelay     = flag.Duration("max-retry-delay", 5*time.Minute, "Максимальная задержка между попытками")
		captchaCooldown   = flag.Duration("captcha-cooldown", 10*time.Minute, "Пауза после обнаружения капчи")
		errorThreshold    = flag.Int("error-threshold", 10, "Количество ошибок подряд для активации защитного режима")
		protectiveDelay   = flag.Duration("protective-delay", 30*time.Second, "Задержка в защитном режиме")
		recoveryDelay     = flag.Duration("recovery-delay", 2*time.Minute, "Пауза для восстановления сессии")
		userAgentRotation = flag.Bool("user-agent-rotation", true, "Ротация User-Agent")
		legacyMode        = flag.Bool("legacy", false, "Использовать старую версию scraper без resilient функций")
	)
	flag.Parse()

	cookie := *cookieFlag
	if cookie == "" {
		cookie = strings.TrimSpace(os.Getenv("COOKIE"))
	}
	cookie = parseCookieEnv(cookie)

	repoRaw := repo.NewRawRepository(db)

	opts := yandex.Options{
		DBRepo:               repoRaw,
		StartURLTmpl:         getStartURLTmpl(*deal),
		City:                 *city,
		StartPage:            *startPage,
		Pages:                *pages,
		SingleURL:            *urlOnly,
		ProxyURL:             *proxyURL,
		DelayMin:             *delayMin,
		DelayMax:             *delayMax,
		Parallelism:          *parallel,
		Cookie:               cookie,
		UseReferer:           *useRef,
		MaxItems:             *maxItems,
		MaxEmptyListingPages: *maxEmpty,
		DealType:             mapDealType(*deal),
	}

	var err error
	if *legacyMode {
		log.Info("using legacy scraper mode")
		err = yandex.RunLegacy(opts, log)
	} else {
		// Используем resilient scraper
		resilientOpts := yandex.ResilientOptions{
			Options:              opts,
			MaxRetries:           *maxRetries,
			BaseRetryDelay:       *baseRetryDelay,
			MaxRetryDelay:        *maxRetryDelay,
			CaptchaCooldown:      *captchaCooldown,
			ErrorThreshold:       *errorThreshold,
			ProtectiveModeDelay:  *protectiveDelay,
			SessionRecoveryDelay: *recoveryDelay,
			UserAgentRotation:    *userAgentRotation,
			ProxyRotation:        parseProxyList(os.Getenv("PROXY_LIST")), // можно передать список прокси через env
		}

		scraper := yandex.NewResilientScraper(resilientOpts, log)
		err = scraper.RunResilient()
	}

	if err != nil {
		log.Error("scrape run: ", zap.Error(err))
	}

	// storage.Migrate(db, log)
	_ = db.WithContext(context.Background()).Exec("SELECT 1").Error
	storage.CloseDB(db, log)
}

// parseProxyList парсит список прокси из строки, разделенной запятыми
func parseProxyList(proxyListStr string) []string {
	if strings.TrimSpace(proxyListStr) == "" {
		return []string{}
	}

	proxies := strings.Split(proxyListStr, ",")
	result := make([]string, 0, len(proxies))

	for _, proxy := range proxies {
		proxy = strings.TrimSpace(proxy)
		if proxy != "" {
			result = append(result, proxy)
		}
	}

	return result
}

// parseCookieEnv принимает либо готовый заголовок Cookie (одной строкой),
// либо содержимое файла в формате "# Netscape HTTP Cookie File" (много строк)
// и возвращает корректный HTTP-заголовок Cookie (одной строкой "k=v; k2=v2").
func parseCookieEnv(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// уберём оборачивающие кавычки, если они остались из .env
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		s = strings.TrimSuffix(strings.TrimPrefix(s, s[:1]), s[len(s)-1:])
	}
	// Если это Netscape Cookie File (multi-line с табами) — распарсим
	if strings.HasPrefix(s, "# Netscape HTTP Cookie File") || strings.Contains(s, "\t") || strings.Contains(s, "\n") {
		lines := strings.Split(s, "\n")
		pairs := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// Формат: domain \t flag \t path \t secure \t expires \t name \t value
			fields := strings.Split(line, "\t")
			if len(fields) < 7 {
				// fallback: по пробелам
				fields = strings.Fields(line)
			}
			if len(fields) >= 7 {
				name := strings.TrimSpace(fields[5])
				value := strings.TrimSpace(fields[6])
				if name != "" {
					pairs = append(pairs, fmt.Sprintf("%s=%s", name, value))
				}
			}
		}
		return strings.TrimSpace(strings.Join(pairs, "; "))
	}
	// Иначе считаем, что это уже готовый Cookie header; уберём переводы строк
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func getStartURLTmpl(deal string) string {
	switch deal {
	case "sale":
		return "https://realty.yandex.ru/{city}/kupit/kvartira/?page={page}"
	case "rent":
		return "https://realty.yandex.ru/{city}/snyat/kvartira/?page={page}"
	default:
		return "https://realty.yandex.ru/{city}/kupit/kvartira/?page={page}"
	}
}

func mapDealType(flag string) string {
	switch flag {
	case "sale":
		return "sale"
	case "rent":
		return "rent_long"
	default:
		return "sale"
	}
}
