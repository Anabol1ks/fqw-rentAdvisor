package handlers

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"go_back/internal/cache"
	"go_back/internal/geocode"
)

type GeocodeHandler struct {
	geocoderClient geocode.GeocoderClient
	cache          *cache.RedisClient
}

func NewGeocodeHandler(geocoderClient geocode.GeocoderClient, cache *cache.RedisClient) *GeocodeHandler {
	return &GeocodeHandler{
		geocoderClient: geocoderClient,
		cache:          cache,
	}
}

// AddressSuggestionResponse ответ с подсказками адресов
type AddressSuggestionResponse struct {
	Suggestions []geocode.AddressSuggestion `json:"suggestions"`
	FromCache   bool                        `json:"from_cache"`
}

// SuggestAddress godoc
// @Summary      Подсказки адресов
// @Description  Возвращает список подсказок адресов по запросу пользователя (через Yandex Geocoder)
// @Tags         geocode
// @Accept       json
// @Produce      json
// @Param        query query     string true "Поисковый запрос" minlength(3)
// @Param        limit query     int false "Максимум результатов" default(5) minimum(1) maximum(10)
// @Success      200  {object}   AddressSuggestionResponse
// @Failure      400  {object}   ErrorResponse
// @Failure      500  {object}   ErrorResponse
// @Router       /api/v1/geocode/suggest [get]
func (h *GeocodeHandler) SuggestAddress(c *gin.Context) {
	// Проверяем, что геокодер инициализирован
	if h.geocoderClient == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: "geocoding service is not configured"})
		return
	}

	query := c.Query("query")
	if query == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "query parameter is required"})
		return
	}

	if len(query) < 3 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "query must be at least 3 characters"})
		return
	}

	limitStr := c.DefaultQuery("limit", "5")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 10 {
		limit = 5
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Генерируем ключ кэша
	cacheKey := generateCacheKey(query, limit)
	fromCache := false

	// Пробуем получить из кэша
	var suggestions []geocode.AddressSuggestion
	if h.cache != nil {
		found, err := h.cache.GetJSON(ctx, cacheKey, &suggestions)
		if err == nil && found {
			fromCache = true
			c.JSON(http.StatusOK, AddressSuggestionResponse{
				Suggestions: suggestions,
				FromCache:   fromCache,
			})
			return
		}
	}

	// Если не нашли в кэше, запрашиваем у геокодера
	suggestions, err = h.geocoderClient.SuggestAddresses(ctx, query, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error: fmt.Sprintf("failed to fetch address suggestions: %v", err),
		})
		return
	}

	// Сохраняем в кэш на 24 часа
	if h.cache != nil {
		_ = h.cache.Set(ctx, cacheKey, suggestions, 24*time.Hour)
	}

	c.JSON(http.StatusOK, AddressSuggestionResponse{
		Suggestions: suggestions,
		FromCache:   fromCache,
	})
}

// generateCacheKey генерирует ключ для кэширования
func generateCacheKey(query string, limit int) string {
	data := fmt.Sprintf("geocode:%s:%d", query, limit)
	hash := md5.Sum([]byte(data))
	return fmt.Sprintf("geo:%x", hash)
}
