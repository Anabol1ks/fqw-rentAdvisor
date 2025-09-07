package main

import (
	"skripe/config"
	"skripe/internal/storage"

	"github.com/joho/godotenv"

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

	storage.Migrate(db, log)

	storage.CloseDB(db, log)
}
