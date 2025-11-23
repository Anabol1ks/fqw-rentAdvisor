package main

import (
	"flag"

	"github.com/Anabol1ks/rentAdvisor-utils-go/logger"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"skripe/config"
	"skripe/internal/enrich"
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

	city := flag.String("city", "", "Ограничить обогащение конкретным городом")
	flag.Parse()

	if err := enrich.RunCity(db, log, *city); err != nil {
		log.Error("enrich run", zap.Error(err))
	}
}
