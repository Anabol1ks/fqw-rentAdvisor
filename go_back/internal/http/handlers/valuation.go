package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"go_back/internal/mlclient"
)

type ValuationHandler struct {
	ml *mlclient.Client
}

func NewValuationHandler(ml *mlclient.Client) *ValuationHandler {
	return &ValuationHandler{ml: ml}
}

// То, что приходит от фронта
type AddressValuationRequest struct {
	City          string   `json:"city" binding:"required"`
	Address       string   `json:"address" binding:"required"`
	Rooms         int      `json:"rooms" binding:"required"`
	AreaTotal     float64  `json:"area_total" binding:"required"`
	AreaLiving    *float64 `json:"area_living"`
	AreaKitchen   *float64 `json:"area_kitchen"`
	Floor         *int     `json:"floor"`
	FloorsTotal   *int     `json:"floors_total"`
	YearBuilt     *int     `json:"year_built"`
	HouseMaterial *string  `json:"house_material"`
	Condition     *string  `json:"condition"`
	WithText      bool     `json:"with_text"` // можно по умолчанию true на фронте
}

func (h *ValuationHandler) PredictAddress(c *gin.Context) {
	var req AddressValuationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mlReq := mlclient.PredictAddressRequest{
		City:          req.City,
		Address:       req.Address,
		Rooms:         req.Rooms,
		AreaTotal:     req.AreaTotal,
		AreaLiving:    req.AreaLiving,
		AreaKitchen:   req.AreaKitchen,
		Floor:         req.Floor,
		FloorsTotal:   req.FloorsTotal,
		YearBuilt:     req.YearBuilt,
		HouseMaterial: req.HouseMaterial,
		Condition:     req.Condition,
		WithText:      req.WithText,
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	report, err := h.ml.PredictAddress(ctx, mlReq)
	if err != nil {
		// тут лучше использовать твой логгер
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	// на первом этапе можно просто прокинуть report как есть
	c.JSON(http.StatusOK, report)
}

// CheckMLHealth проверяет доступность ML-сервиса
func (h *ValuationHandler) CheckMLHealth(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	health, err := h.ml.Health(ctx)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, health)
}
