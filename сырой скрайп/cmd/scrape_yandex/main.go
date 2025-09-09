package main

import (
	"context"
	"flag"
	"skripe/config"
	"skripe/internal/repo"
	"skripe/internal/scrapers/yandex"
	"skripe/internal/storage"
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
		city      = flag.String("city", "moskva", "Город в URL (пример: moskva, peterburg)")
		startPage = flag.Int("start", 1, "Стартовая страница")
		pages     = flag.Int("pages", 1, "Сколько страниц обойти")
		proxyURL  = flag.String("proxy", "", "HTTP/HTTPS proxy (опционально)")
		delayMin  = flag.Duration("delay-min", 1200*time.Millisecond, "Минимальная задержка между запросами")
		delayMax  = flag.Duration("delay-max", 2500*time.Millisecond, "Максимальная задержка между запросами")
		parallel  = flag.Int("parallel", 2, "Параллелизм")
		maxItems  = flag.Int("max-items", 0, "Максимум карточек для сбора (0 = без лимита)")
		maxEmpty  = flag.Int("max-empty-pages", 0, "Остановить после N подряд пустых страниц выдачи (0 = игнорировать)")
		deal      = flag.String("deal", "sale", "sale|rent")
		cookie    = flag.String("cookie", "", "Строка Cookie для realty.yandex.ru (опционально)")
		useRef    = flag.Bool("use-referer", true, "Включить заголовок Referer для Colly")
	)
	flag.Parse()

	repoRaw := repo.NewRawRepository(db)

	opts := yandex.Options{
		DBRepo:               repoRaw,
		StartURLTmpl:         getStartURLTmpl(*deal),
		City:                 *city,
		StartPage:            *startPage,
		Pages:                *pages,
		ProxyURL:             *proxyURL,
		DelayMin:             *delayMin,
		DelayMax:             *delayMax,
		Parallelism:          *parallel,
		Cookie:               *cookie,
		UseReferer:           *useRef,
		MaxItems:             *maxItems,
		MaxEmptyListingPages: *maxEmpty,
		DealType:             mapDealType(*deal),
	}

	if err := yandex.Run(opts, log); err != nil {
		log.Error("scrape run: ", zap.Error(err))
	}

	// storage.Migrate(db, log)
	_ = db.WithContext(context.Background()).Exec("SELECT 1").Error
	storage.CloseDB(db, log)
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
