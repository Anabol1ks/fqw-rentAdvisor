package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"go_back/internal/mlclient"
	"go_back/internal/models"
	"go_back/internal/reportpdf"
	repositories "go_back/internal/repository"
)

type ValuationHandler struct {
	ml   *mlclient.Client
	repo repositories.ValuationReportRepository
}

func NewValuationHandler(ml *mlclient.Client, repo repositories.ValuationReportRepository) *ValuationHandler {
	return &ValuationHandler{ml: ml, repo: repo}
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
	Rooms         *int     `json:"rooms" binding:"required" example:"2"`
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
	URL          *string  `json:"url,omitempty" example:"https://realty.yandex.ru/offer/123456/"` // <-- ДОБАВЛЕНО
}

// AddressValuationResponse — то, что отдаём фронту
type AddressValuationResponse struct {
	ReportID    string                              `json:"report_id"`
	Object      AddressValuationObjectSummary       `json:"object"`
	Price       AddressValuationPriceSummary        `json:"price"`
	Text        *AddressValuationTextSummary        `json:"text,omitempty"`
	Comparables []AddressValuationComparableSummary `json:"comparables,omitempty"`
}

// PredictAddress godoc
// @Summary      Оценка аренды квартиры по адресу
// @Description  Вызывает ML-сервис RealVal для расчёта рыночной ставки аренды и сохраняет полный отчёт
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
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if req.Rooms == nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "rooms is required"})
		return
	}

	mlReq := mlclient.PredictAddressRequest{
		City:          req.City,
		Address:       req.Address,
		Rooms:         *req.Rooms,
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	report, err := h.ml.PredictAddress(ctx, mlReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: err.Error()})
		return
	}

	// маршалим полный отчёт в JSON для хранения
	raw, err := json.Marshal(report)
	if err != nil {
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: "failed to marshal ML report"})
		return
	}

	vr := &models.ValuationReport{
		// ID заполнит репозиторий (uuid.New())
		City:            report.Object.City,
		Address:         report.Object.Address,
		DealType:        report.Pricing.DealType,
		PredictionRub:   report.Pricing.PredictionRub,
		IntervalLowRub:  report.Pricing.IntervalLowRub,
		IntervalHighRub: report.Pricing.IntervalHighRub,
		RawReport:       raw,
	}

	if err := h.repo.Save(ctx, vr); err != nil {
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: err.Error()})
		return
	}

	// чтобы фронт мог потом запрашивать по ID, переопределим ReportID, если нужно
	// можно либо использовать UUID из Python, либо тот, что в БД.
	// Я бы делал так: в ответе держим обе сущности согласованными.
	report.ReportID = vr.ID.String()

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

// GetValuationByID godoc
// @Summary      Получить краткий отчёт об оценке по ID
// @Description  Читает сохранённый отчёт из БД и возвращает краткую версию для фронта
// @Tags         valuation
// @Accept       json
// @Produce      json
// @Param        id   path      string true "ID отчёта (UUID)"
// @Success      200  {object}  AddressValuationResponse
// @Failure      404  {object}  ErrorResponse
// @Failure      400  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /api/v1/valuation/{id} [get]
func (h *ValuationHandler) GetValuationByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	vr, err := h.repo.GetByID(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	if vr == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "report not found"})
		return
	}

	var full mlclient.Report
	if err := json.Unmarshal(vr.RawReport, &full); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to decode stored report"})
		return
	}

	// синхронизируем ReportID с ID из БД (на всякий случай)
	full.ReportID = vr.ID.String()

	thin := mapReportToAddressResponse(&full)
	c.JSON(http.StatusOK, thin)
}

func mapReportToAddressResponse(r *mlclient.Report) AddressValuationResponse {
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

	price := AddressValuationPriceSummary{
		PredictionRub:   r.Pricing.PredictionRub,
		IntervalLowRub:  r.Pricing.IntervalLowRub,
		IntervalHighRub: r.Pricing.IntervalHighRub,
		Currency:        r.Pricing.Currency,
		DealType:        r.Pricing.DealType,
	}

	var text *AddressValuationTextSummary
	if r.Text != nil {
		text = &AddressValuationTextSummary{
			SummaryShort:   r.Text.SummaryShort,
			SummaryLong:    r.Text.SummaryLong,
			FactorsSummary: append([]string{}, r.Text.FactorsSummary...),
		}
	}

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
			URL:          c.URL, // <-- ДОБАВЛЕНО
		})
	}

	return AddressValuationResponse{
		ReportID:    r.ReportID,
		Object:      obj,
		Price:       price,
		Text:        text,
		Comparables: comparables,
	}
}

// GetValuationPDF godoc
// @Summary      PDF-отчет по оценке
// @Description  Возвращает PDF-файл с полным отчетом по оценке аренды
// @Tags         valuation
// @Produce      application/pdf
// @Param        id   path      string true "ID отчёта (UUID)"
// @Success      200  {file}    file
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /api/v1/valuation/{id}/pdf [get]
func (h *ValuationHandler) GetValuationPDF(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	vr, err := h.repo.GetByID(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	if vr == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "report not found"})
		return
	}

	var full mlclient.Report
	if err := json.Unmarshal(vr.RawReport, &full); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to decode stored report"})
		return
	}
	full.ReportID = vr.ID.String()

	pdfBytes, err := reportpdf.RenderReportPDF(&full, reportpdf.Options{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "failed to render pdf",
			"detail": err.Error(), // <-- покажет реальную ошибку
		})
		return
	}

	filename := fmt.Sprintf("report_%s.pdf", vr.ID.String())
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Data(http.StatusOK, "application/pdf", pdfBytes)
}

type ValuationListItem struct {
	ID              string    `json:"id" example:"d9a3f3b6-..."`
	CreatedAt       time.Time `json:"created_at"`
	City            string    `json:"city" example:"Москва"`
	Address         string    `json:"address" example:"Нежинская улица, 1к1"`
	DealType        string    `json:"deal_type" example:"rent_long"`
	PredictionRub   float64   `json:"prediction_rub" example:"165644.8"`
	IntervalLowRub  float64   `json:"interval_low_rub" example:"120077.1"`
	IntervalHighRub float64   `json:"interval_high_rub" example:"228504.7"`
	SummaryShort    string    `json:"summary_short,omitempty" example:"Квартира в городе Москва..."`
}

// ответ со списком + простая пагинация
type ValuationListResponse struct {
	Items  []ValuationListItem `json:"items"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}

// ListValuations godoc
// @Summary      Список сохранённых отчётов
// @Description  Возвращает общий список сохранённых оценок (thin-формат)
// @Tags         valuation
// @Accept       json
// @Produce      json
// @Param        limit   query     int false "Максимум записей" default(20)
// @Param        offset  query     int false "Смещение" default(0)
// @Success      200  {object}    ValuationListResponse
// @Failure      500  {object}    ErrorResponse
// @Router       /api/v1/valuation [get]
func (h *ValuationHandler) ListValuations(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit <= 0 {
		limit = 20
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		offset = 0
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	items, err := h.repo.List(ctx, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	result := ValuationListResponse{
		Items:  make([]ValuationListItem, 0, len(items)),
		Limit:  limit,
		Offset: offset,
	}

	for _, vr := range items {
		li := ValuationListItem{
			ID:              vr.ID.String(),
			CreatedAt:       vr.CreatedAt,
			City:            vr.City,
			Address:         vr.Address,
			DealType:        vr.DealType,
			PredictionRub:   vr.PredictionRub,
			IntervalLowRub:  vr.IntervalLowRub,
			IntervalHighRub: vr.IntervalHighRub,
		}

		// попробуем достать summary_short из raw_report (если есть текст)
		var full mlclient.Report
		if err := json.Unmarshal(vr.RawReport, &full); err == nil && full.Text != nil {
			li.SummaryShort = full.Text.SummaryShort
		}

		result.Items = append(result.Items, li)
	}

	c.JSON(http.StatusOK, result)
}
