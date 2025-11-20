package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// GeocoderClient интерфейс для геокодеров
type GeocoderClient interface {
	SuggestAddresses(ctx context.Context, query string, limit int) ([]AddressSuggestion, error)
}

// YandexClient для работы с Yandex Geocoder API
type YandexClient struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// AddressSuggestion представляет один вариант адреса
type AddressSuggestion struct {
	Address     string  `json:"address"`
	Description string  `json:"description"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	// Примечание: Yandex Geocoder API не предоставляет данные о доме (этажность, год, материал)
	// Для получения этих данных нужно использовать другие API (например, Yandex Maps Places API)
}

// YandexGeocoderResponse ответ от Yandex Geocoder API
type YandexGeocoderResponse struct {
	Response struct {
		GeoObjectCollection struct {
			FeatureMember []struct {
				GeoObject struct {
					MetaDataProperty struct {
						GeocoderMetaData struct {
							Text    string `json:"text"`
							Kind    string `json:"kind"`
							Address struct {
								Formatted string `json:"formatted"`
							} `json:"Address"`
						} `json:"GeocoderMetaData"`
					} `json:"metaDataProperty"`
					Description string `json:"description"`
					Point       struct {
						Pos string `json:"pos"`
					} `json:"Point"`
				} `json:"GeoObject"`
			} `json:"featureMember"`
		} `json:"GeoObjectCollection"`
	} `json:"response"`
}

// NewYandexClient создает новый клиент для Yandex Geocoder
func NewYandexClient(apiKey string) *YandexClient {
	return &YandexClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL: "https://geocode-maps.yandex.ru/1.x/",
	}
}

// SuggestAddresses ищет адреса по запросу
func (c *YandexClient) SuggestAddresses(ctx context.Context, query string, limit int) ([]AddressSuggestion, error) {
	if query == "" {
		return []AddressSuggestion{}, nil
	}

	if limit <= 0 {
		limit = 5
	}

	// Добавляем "Москва, Россия" для более точного поиска
	searchQuery := fmt.Sprintf("%s, Москва, Россия", query)

	// Строим URL с параметрами
	params := url.Values{}
	params.Set("apikey", c.apiKey)
	params.Set("geocode", searchQuery)
	params.Set("format", "json")
	params.Set("results", fmt.Sprintf("%d", limit))
	params.Set("kind", "house") // Фокус на домах, а не районах

	reqURL := c.baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yandex geocoder returned status %d: %s", resp.StatusCode, string(body))
	}

	var geoResp YandexGeocoderResponse
	if err := json.Unmarshal(body, &geoResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w, body: %s", err, string(body))
	}

	// Конвертируем в наш формат
	suggestions := make([]AddressSuggestion, 0, len(geoResp.Response.GeoObjectCollection.FeatureMember))

	for _, member := range geoResp.Response.GeoObjectCollection.FeatureMember {
		geoObj := member.GeoObject
		metaData := geoObj.MetaDataProperty.GeocoderMetaData

		// Парсим координаты (формат: "longitude latitude")
		var lon, lat float64
		fmt.Sscanf(geoObj.Point.Pos, "%f %f", &lon, &lat)

		suggestion := AddressSuggestion{
			Address:     metaData.Address.Formatted,
			Description: geoObj.Description,
			Latitude:    lat,
			Longitude:   lon,
		}

		suggestions = append(suggestions, suggestion)
	}

	return suggestions, nil
}
