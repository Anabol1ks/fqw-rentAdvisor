package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"skripe/config"
	"skripe/internal/repo"
	"skripe/internal/scrapers/cian"
	"skripe/internal/storage"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/Anabol1ks/rentAdvisor-utils-go/logger"
)

func main() {
	_ = godotenv.Load()
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
		cookieFlag = flag.String("cookie", "", "Строка Cookie для www.cian.ru (опционально)")
		useRef     = flag.Bool("use-referer", true, "Включить заголовок Referer для Colly")
	)
	flag.Parse()

	cookie := *cookieFlag
	if cookie == "" {
		cookie = strings.TrimSpace(os.Getenv("COOKIE_CIAN"))
	}
	cookie = parseCookieEnv(cookie)

	repoRaw := repo.NewRawRepository(db)

	opts := cian.Options{
		DBRepo:               repoRaw,
		StartURLTmpl:         getStartURLTmpl(*deal, *city),
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

	if err := cian.Run(opts, log); err != nil {
		log.Error("scrape cian run", zap.Error(err))
	}

	_ = db.WithContext(context.Background()).Exec("SELECT 1").Error
	storage.CloseDB(db, log)
}

// parseCookieEnv: такой же, как в яндекс-скраперe
func parseCookieEnv(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		s = strings.TrimSuffix(strings.TrimPrefix(s, s[:1]), s[len(s)-1:])
	}
	if strings.HasPrefix(s, "# Netscape HTTP Cookie File") || strings.Contains(s, "\t") || strings.Contains(s, "\n") {
		lines := strings.Split(s, "\n")
		pairs := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) < 7 {
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
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func getStartURLTmpl(deal, city string) string {
	// Для простоты используем движок выдачи cat.php. region=1 (Москва) по умолчанию;
	// если потребуется другой регион, можно расширить маппинг по city → regionId.
	// Строка {page} будет заменена при обходе.
	// Пример карточек: https://www.cian.ru/cat.php?deal_type=sale&engine_version=2&p=1&region=1
	switch deal {
	case "sale":
		return "https://www.cian.ru/cat.php?deal_type=sale&engine_version=2&p={page}&region=1"
	case "rent":
		return "https://www.cian.ru/cat.php?deal_type=rent&engine_version=2&p={page}&region=1"
	default:
		return "https://www.cian.ru/cat.php?deal_type=sale&engine_version=2&p={page}&region=1"
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
