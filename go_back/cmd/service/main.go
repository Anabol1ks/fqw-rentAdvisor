package main

import (
	_ "go_back/docs"
	"go_back/internal/cache"
	"go_back/internal/config"
	"go_back/internal/database"
	"go_back/internal/geocode"
	httpapi "go_back/internal/http"
	"go_back/internal/http/handlers"
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

	// Инициализация геокодера (Yandex или Nominatim)
	var geocoderClient geocode.GeocoderClient
	if cfg.YandexGeoAPI != "" {
		geocoderClient = geocode.NewYandexClient(cfg.YandexGeoAPI)
		log.Info("Yandex Geocoder client initialized")
	} else {
		geocoderClient = geocode.NewNominatimClient()
		log.Info("Nominatim (OpenStreetMap) Geocoder client initialized (free alternative)")
	}

	// Инициализация Redis (опционально)
	var redisClient *cache.RedisClient
	if cfg.RedisAddr != "" {
		var err error
		redisClient, err = cache.NewRedisClient(
			cfg.RedisAddr,
			cfg.RedisPassword,
			cfg.RedisDB,
			cfg.RedisCachePrefix,
		)
		if err != nil {
			log.Warn("Failed to connect to Redis, caching disabled",
				zap.Error(err),
			)
		} else {
			log.Info("Redis client initialized")
			defer redisClient.Close()
		}
	} else {
		log.Info("Redis not configured, caching disabled")
	}

	// Создаем geocode handler
	geocodeHandler := handlers.NewGeocodeHandler(geocoderClient, redisClient)

	r := httpapi.SetupRouter(cfg, repo, geocodeHandler, db, log)
	port := ":" + cfg.Port
	if err := r.Run(port); err != nil {
		log.Fatal("failed to run http server", zap.Error(err))
	}
}
