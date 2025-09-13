package main

import (
	"github.com/Anabol1ks/rentAdvisor-utils-go/logger"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"skripe/config"
	"skripe/internal/dedupe"
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

	if err := dedupe.Run(db, log); err != nil {
		log.Error("dedupe run", zap.Error(err))
	}
}
