package main

import (
	_ "go_back/docs"
	"go_back/internal/config"
	"go_back/internal/database"
	httpapi "go_back/internal/http"
	"go_back/internal/logger"
	"go_back/internal/models"
	"go_back/internal/repository"
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

	db.AutoMigrate(&models.ValuationReport{})

	repo := repository.NewValuationReportRepository(db)

	r := httpapi.SetupRouter(cfg, repo)
	port := ":" + cfg.Port
	if err := r.Run(port); err != nil {
		log.Fatal("failed to run http server", zap.Error(err))
	}
}
