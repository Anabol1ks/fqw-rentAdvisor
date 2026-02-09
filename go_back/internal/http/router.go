package httpapi

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"go_back/internal/admin"
	"go_back/internal/config"
	"go_back/internal/http/handlers"
	"go_back/internal/mlclient"
	"go_back/internal/repository"
)

func SetupRouter(cfg *config.Config, repo repository.ValuationReportRepository, geocodeHandler *handlers.GeocodeHandler, db *gorm.DB, log *zap.Logger) *gin.Engine {
	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Admin-Key"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	r.GET("/health", func(c *gin.Context) {
		c.String(200, "ok")
	})

	ml := mlclient.New(cfg.MLServiceURL, time.Duration(cfg.MLTimeoutSec)*time.Second)
	valuationHandler := handlers.NewValuationHandler(ml, repo)

	api := r.Group("/api/v1")
	{
		valuation := api.Group("/valuation")
		{
			valuation.POST("/address", valuationHandler.PredictAddress)
			valuation.GET("/ml-health", valuationHandler.CheckMLHealth)
			valuation.GET("", valuationHandler.ListValuations)
			valuation.GET("/:id", valuationHandler.GetValuationByID)
			valuation.GET("/:id/pdf", valuationHandler.GetValuationPDF)
		}

		geocode := api.Group("/geocode")
		{
			geocode.GET("/suggest", geocodeHandler.SuggestAddress)
		}
	}

	// ===== Admin Panel Routes =====
	taskRunner := admin.NewTaskRunner(log)
	adminHandler := handlers.NewAdminHandler(taskRunner, db, cfg, log)

	adminGroup := r.Group("/api/v1/admin")
	if cfg.AdminKey != "" {
		adminGroup.Use(handlers.AdminAuthMiddleware(cfg.AdminKey))
	}
	{
		adminGroup.GET("/data/stats", adminHandler.GetDataStats)

		// Scraping
		adminGroup.POST("/scrape/start", adminHandler.StartScrape)
		adminGroup.GET("/scrape/config", adminHandler.GetScrapeConfig)
		adminGroup.POST("/scrap/:command", adminHandler.RunScrapCommand)

		// ML Pipeline
		adminGroup.POST("/ml/:command", adminHandler.RunMLCommand)
		adminGroup.POST("/ml-server/restart", adminHandler.RestartMLServer)

		// Tasks
		adminGroup.GET("/tasks", adminHandler.ListTasks)
		adminGroup.GET("/tasks/:id", adminHandler.GetTask)
		adminGroup.POST("/tasks/:id/stop", adminHandler.StopTask)
		adminGroup.GET("/tasks/:id/logs", adminHandler.StreamTaskLogs)

		// Cookies
		adminGroup.GET("/cookies", adminHandler.GetCookies)
		adminGroup.PUT("/cookies", adminHandler.UpdateCookies)
	}

	return r
}
