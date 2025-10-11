package main

import (
	"skripe/config"
	"skripe/internal/storage"

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
	sql := []string{
		`UPDATE listing SET is_active=false
		  WHERE deal_type='rent_long' AND is_active AND last_seen < now()-interval '90 days'`,
		`UPDATE listing SET is_active=false
		  WHERE deal_type='sale' AND is_active AND last_seen < now()-interval '150 days'`,
	}
	for _, q := range sql {
		if err := db.Exec(q).Error; err != nil {
			log.Fatal("failed to execute query", zap.Error(err))
		}
	}
	log.Info("deactivate done")
	storage.CloseDB(db, log)
}
