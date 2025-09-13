package main

import (
	"flag"
	"time"

	"github.com/Anabol1ks/rentAdvisor-utils-go/logger"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"skripe/config"
	"skripe/internal/geocode"
	"skripe/internal/storage"
)

func main() {
	_ = godotenv.Load()
	_ = logger.Init(true)
	defer logger.Sync()
	log := logger.L()

	cfg := config.Load(log)
	db := storage.ConnectDB(&cfg.DB, log)
	defer storage.CloseDB(db, log)

	workers := flag.Int("workers", 2, "Workers count")
	sleep := flag.Duration("sleep", 800*time.Millisecond, "Sleep between requests per success")
	defCity := flag.String("city", "Москва", "Default city to prefix")
	limit := flag.Int("limit", 0, "Max items to process (0=all)")
	ua := flag.String("ua", "rentAdvisor/1.0 (+contact@example.com)", "User-Agent for Nominatim")
	email := flag.String("email", "contact@example.com", "Contact email for Nominatim")
	flag.Parse()

	prov := &geocode.Nominatim{UserAgent: *ua, Email: *email}
	if err := geocode.RunBatch(db, log, prov, geocode.Options{
		Workers:     *workers,
		Limit:       *limit,
		Sleep:       *sleep,
		DefaultCity: *defCity,
	}); err != nil {
		log.Error("geocode run", zap.Error(err))
	}
}
