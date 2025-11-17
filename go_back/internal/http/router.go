package httpapi

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"go_back/internal/config"
	"go_back/internal/http/handlers"
	"go_back/internal/mlclient"
)

func SetupRouter(cfg *config.Config) *gin.Engine {
	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	r.GET("/health", func(c *gin.Context) {
		c.String(200, "ok")
	})

	ml := mlclient.New(cfg.MLServiceURL, time.Duration(cfg.MLTimeoutSec)*time.Second)
	valuationHandler := handlers.NewValuationHandler(ml)

	api := r.Group("/api/v1")
	{
		valuation := api.Group("/valuation")
		{
			valuation.POST("/address", valuationHandler.PredictAddress)
			valuation.GET("/ml-health", valuationHandler.CheckMLHealth)
		}
	}

	return r
}
