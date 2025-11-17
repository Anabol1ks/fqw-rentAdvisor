package main

import (
	_ "go_back/docs"
	"go_back/internal/config"
	"go_back/internal/database"
	httpapi "go_back/internal/http"
	"go_back/internal/logger"
	"os"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	_ = godotenv.Load()
	isDev := os.Getenv("ENV") == "development"
	if err := logger.Init(isDev); err != nil {
		panic(err)
	}
	defer logger.Sync()
	log := logger.L()

	cfg := config.Load(log)

	db := database.ConnectDB(cfg, log)
	defer database.CloseDB(db, log)

	r := httpapi.SetupRouter(cfg)
	port := ":" + cfg.Port
	if err := r.Run(port); err != nil {
		log.Fatal("failed to run http server", zap.Error(err))
	}
}
