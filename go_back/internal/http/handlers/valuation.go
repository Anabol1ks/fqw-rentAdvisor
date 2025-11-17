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

// ==== DTO для ошибок (для Swagger и единообразия) ====

type ErrorResponse struct {
	Error string `json:"error" example:"something went wrong"`
}

// ==== Входной DTO (от фронта) ====

// AddressValuationRequest то, что присылает фронт
type AddressValuationRequest struct {
	City          string   `json:"city" binding:"required" example:"Москва"`
	Address       string   `json:"address" binding:"required" example:"Нежинская улица, 1к1"`
	Rooms         int      `json:"rooms" binding:"required" example:"2"`
	AreaTotal     float64  `json:"area_total" binding:"required" example:"90"`
	AreaLiving    *float64 `json:"area_living,omitempty" example:"65"`
	AreaKitchen   *float64 `json:"area_kitchen,omitempty" example:"20"`
	Floor         *int     `json:"floor,omitempty" example:"24"`
	FloorsTotal   *int     `json:"floors_total,omitempty" example:"31"`
	YearBuilt     *int     `json:"year_built,omitempty" example:"2008"`
	HouseMaterial *string  `json:"house_material,omitempty" example:"монолит"`
	Condition     *string  `json:"condition,omitempty" example:"без отделки"`
	WithText      bool     `json:"with_text" example:"true"`
}

// ==== THIN DTO ДЛЯ ОТВЕТА ФРОНТУ ====

// AddressValuationObjectSummary — краткая инфа об объекте
type AddressValuationObjectSummary struct {
	City          string  `json:"city" example:"Москва"`
	Address       string  `json:"address" example:"Нежинская улица, 1к1"`
	Rooms         int     `json:"rooms" example:"2"`
	AreaTotal     float64 `json:"area_total" example:"90"`
	Floor         *int    `json:"floor,omitempty" example:"24"`
	FloorsTotal   *int    `json:"floors_total,omitempty" example:"31"`
	YearBuilt     *int    `json:"year_built,omitempty" example:"2008"`
	HouseMaterial *string `json:"house_material,omitempty" example:"монолит"`
}

// AddressValuationPriceSummary — блок с ценой и интервалом
type AddressValuationPriceSummary struct {
	PredictionRub   float64 `json:"prediction_rub" example:"165644.8"`
	IntervalLowRub  float64 `json:"interval_low_rub" example:"120077.1"`
	IntervalHighRub float64 `json:"interval_high_rub" example:"228504.7"`
	Currency        string  `json:"currency" example:"RUB"`
	DealType        string  `json:"deal_type" example:"rent_long"`
}

// AddressValuationTextSummary — текстовые объяснения
type AddressValuationTextSummary struct {
	SummaryShort   string   `json:"summary_short" example:"Квартира в городе Москва..."`
	SummaryLong    string   `json:"summary_long" example:"Развёрнутое описание оценки..."`
	FactorsSummary []string `json:"factors_summary"`
}

// AddressValuationComparableSummary — коротко про компараблы
type AddressValuationComparableSummary struct {
	PriceRub     float64  `json:"price_rub" example:"195000"`
	Rooms        *int     `json:"rooms,omitempty" example:"2"`
	AreaTotal    *float64 `json:"area_total,omitempty" example:"83"`
	DistanceKm   *float64 `json:"distance_km,omitempty" example:"0.01"`
	MetroStation *string  `json:"metro_station,omitempty" example:"Славянский бульвар"`
}

// AddressValuationResponse — то, что отдаём фронту
type AddressValuationResponse struct {
	Object      AddressValuationObjectSummary       `json:"object"`
	Price       AddressValuationPriceSummary        `json:"price"`
	Text        *AddressValuationTextSummary        `json:"text,omitempty"`
	Comparables []AddressValuationComparableSummary `json:"comparables,omitempty"`
}

// PredictAddress godoc
// @Summary      Оценка аренды квартиры по адресу
// @Description  Вызывает ML-сервис RealVal для расчёта рыночной ставки аренды и возвращает краткий отчёт
// @Tags         valuation
// @Accept       json
// @Produce      json
// @Param        request body      AddressValuationRequest true "Параметры квартиры для оценки"
// @Success      200     {object}  AddressValuationResponse
// @Failure      400     {object}  ErrorResponse
// @Failure      502     {object}  ErrorResponse
// @Router       /api/v1/valuation/address [post]
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

	thin := mapReportToAddressResponse(report)
	c.JSON(http.StatusOK, thin)
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

func mapReportToAddressResponse(r *mlclient.Report) AddressValuationResponse {
	// объект
	obj := AddressValuationObjectSummary{
		City:          r.Object.City,
		Address:       r.Object.Address,
		Rooms:         r.Object.Rooms,
		AreaTotal:     r.Object.AreaTotal,
		Floor:         r.Object.Floor,
		FloorsTotal:   r.Object.FloorsTotal,
		YearBuilt:     r.Object.YearBuilt,
		HouseMaterial: r.Object.HouseMaterial,
	}

	// цена
	price := AddressValuationPriceSummary{
		PredictionRub:   r.Pricing.PredictionRub,
		IntervalLowRub:  r.Pricing.IntervalLowRub,
		IntervalHighRub: r.Pricing.IntervalHighRub,
		Currency:        r.Pricing.Currency,
		DealType:        r.Pricing.DealType,
	}

	// текст (если есть)
	var text *AddressValuationTextSummary
	if r.Text != nil {
		text = &AddressValuationTextSummary{
			SummaryShort:   r.Text.SummaryShort,
			SummaryLong:    r.Text.SummaryLong,
			FactorsSummary: append([]string{}, r.Text.FactorsSummary...),
		}
	}

	// компараблы: возьмём первые 3
	comparables := make([]AddressValuationComparableSummary, 0, 3)
	for i, c := range r.Comparables {
		if i >= 3 {
			break
		}
		comparables = append(comparables, AddressValuationComparableSummary{
			PriceRub:     c.PriceRub,
			Rooms:        c.Rooms,
			AreaTotal:    c.AreaTotal,
			DistanceKm:   c.DistanceKm,
			MetroStation: c.MetroStation,
		})
	}

	return AddressValuationResponse{
		Object:      obj,
		Price:       price,
		Text:        text,
		Comparables: comparables,
	}
}
