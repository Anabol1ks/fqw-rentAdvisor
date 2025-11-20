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

// Проверка что NominatimClient реализует GeocoderClient
var _ GeocoderClient = (*NominatimClient)(nil)

// NominatimClient для работы с OpenStreetMap Nominatim API (бесплатно)
type NominatimClient struct {
	httpClient *http.Client
	baseURL    string
	userAgent  string
}

// NominatimResponse ответ от Nominatim API
type NominatimResponse []struct {
	PlaceID     int    `json:"place_id"`
	Licence     string `json:"licence"`
	DisplayName string `json:"display_name"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	Address     struct {
		Road         string `json:"road"`
		HouseNumber  string `json:"house_number"`
		Suburb       string `json:"suburb"`
		City         string `json:"city"`
		Municipality string `json:"municipality"`
		State        string `json:"state"`
		Country      string `json:"country"`
	} `json:"address"`
}

// NewNominatimClient создает новый клиент для Nominatim
func NewNominatimClient() *NominatimClient {
	return &NominatimClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL:   "https://nominatim.openstreetmap.org/search",
		userAgent: "RentAdvisor/1.0", // Обязательно для Nominatim
	}
}

// cleanAddressNominatim удаляет префикс из адреса Nominatim
func cleanAddressNominatim(address string) string {
	prefixes := []string{
		"Россия, Москва, ",
		"Россия, Москва,",
		"Москва, Россия, ",
		"Москва, Россия,",
		"Москва, ",
	}
	for _, prefix := range prefixes {
		if len(address) > len(prefix) && address[:len(prefix)] == prefix {
			return address[len(prefix):]
		}
	}
	return address
}

// SuggestAddresses ищет адреса по запросу через Nominatim
func (c *NominatimClient) SuggestAddresses(ctx context.Context, query string, limit int) ([]AddressSuggestion, error) {
	if query == "" {
		return []AddressSuggestion{}, nil
	}

	if limit <= 0 {
		limit = 5
	}

	// Добавляем "Москва" для более точного поиска
	searchQuery := query + ", Москва, Россия"

	params := url.Values{}
	params.Set("q", searchQuery)
	params.Set("format", "json")
	params.Set("addressdetails", "1")
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("countrycodes", "ru")

	reqURL := c.baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)

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
		return nil, fmt.Errorf("nominatim returned status %d: %s", resp.StatusCode, string(body))
	}

	var nomResp NominatimResponse
	if err := json.Unmarshal(body, &nomResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Конвертируем в наш формат
	suggestions := make([]AddressSuggestion, 0, len(nomResp))

	for _, item := range nomResp {
		// Парсим координаты
		var lat, lon float64
		fmt.Sscanf(item.Lat, "%f", &lat)
		fmt.Sscanf(item.Lon, "%f", &lon)

		// Формируем описание
		description := ""
		if item.Address.Suburb != "" {
			description = item.Address.Suburb
		} else if item.Address.Municipality != "" {
			description = item.Address.Municipality
		}

		suggestion := AddressSuggestion{
			Address:     cleanAddressNominatim(item.DisplayName),
			Description: description,
			Latitude:    lat,
			Longitude:   lon,
		}

		suggestions = append(suggestions, suggestion)
	}

	return suggestions, nil
}
