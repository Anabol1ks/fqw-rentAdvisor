package mlclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

// HealthResponse представляет ответ от /health
type HealthResponse struct {
	Status      string `json:"status"`
	Service     string `json:"service"`
	ModelPath   string `json:"model_path"`
	ModelExists bool   `json:"model_exists"`
}

// Health проверяет доступность ML-сервиса
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	url := c.baseURL + "/health"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call ML service health: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ML service health status %d: %s", resp.StatusCode, string(respBody))
	}

	var health HealthResponse
	if err := json.Unmarshal(respBody, &health); err != nil {
		return nil, fmt.Errorf("decode health response: %w", err)
	}

	return &health, nil
}

func (c *Client) PredictAddress(ctx context.Context, req PredictAddressRequest) (*Report, error) {
	url := c.baseURL + "/v1/predict/address"

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call ML service: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ML service status %d: %s", resp.StatusCode, string(respBody))
	}

	var report Report
	if err := json.Unmarshal(respBody, &report); err != nil {
		return nil, fmt.Errorf("decode report: %w", err)
	}

	return &report, nil
}
