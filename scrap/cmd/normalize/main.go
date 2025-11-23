package main

import (
	"flag"
	"skripe/config"
	"skripe/internal/normalize"
	"skripe/internal/storage"
	"time"

	"github.com/Anabol1ks/rentAdvisor-utils-go/logger"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
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
	defer storage.CloseDB(db, log)

	var (
		limit   = flag.Int("limit", 0, "Сколько сырых записей обработать (0 = все)")
		sleepMs = flag.Int("sleep-ms", 0, "Пауза между батчами (для снижения нагрузки)")
		batch   = flag.Int("batch", 500, "Размер батча выборки из listing_raw")
	)
	flag.Parse()

	opts := normalize.Options{
		Limit:           *limit,
		BatchSize:       *batch,
		SleepBetween:    time.Duration(*sleepMs) * time.Millisecond,
		DefaultCity:     "Москва",
		DailyToMonthlyK: 30.0,
	}

	if err := normalize.Run(db, log, opts); err != nil {
		log.Error("normalize run", zap.Error(err))
	}
}
