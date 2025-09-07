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
		snaps     = flag.String("snapshots", "snapshots/yandex", "Каталог для HTML-снапшотов")
		proxyURL  = flag.String("proxy", "", "HTTP/HTTPS proxy (опционально)")
		delayMin  = flag.Duration("delay-min", 1200*time.Millisecond, "Минимальная задержка между запросами")
		delayMax  = flag.Duration("delay-max", 2500*time.Millisecond, "Максимальная задержка между запросами")
		parallel  = flag.Int("parallel", 2, "Параллелизм")
	)
	flag.Parse()

	repoRaw := repo.NewRawRepository(db)

	opts := yandex.Options{
		DBRepo:       repoRaw,
		StartURLTmpl: "https://realty.yandex.ru/{city}/kupit/kvartira/?page={page}",
		City:         *city,
		StartPage:    *startPage,
		Pages:        *pages,
		SnapshotDir:  *snaps,
		ProxyURL:     *proxyURL,
		DelayMin:     *delayMin,
		DelayMax:     *delayMax,
		Parallelism:  *parallel,
	}

	if err := yandex.Run(opts, log); err != nil {
		log.Error("scrape run: ", zap.Error(err))
	}

	// storage.Migrate(db, log)
	_ = db.WithContext(context.Background()).Exec("SELECT 1").Error
	storage.CloseDB(db, log)
}
