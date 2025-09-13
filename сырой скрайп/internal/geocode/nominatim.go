package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Nominatim struct {
	BaseURL    string // https://nominatim.openstreetmap.org
	UserAgent  string // обязательный корректный UA
	Email      string // по правилам Nominatim лучше указать email
	HTTPClient *http.Client
}

type nomResp struct {
	Lat  string `json:"lat"`
	Lon  string `json:"lon"`
	Type string `json:"type"`
}

func (n *Nominatim) Geocode(ctx context.Context, q string) (*Result, error) {
	if n.BaseURL == "" {
		n.BaseURL = "https://nominatim.openstreetmap.org"
	}
	if n.HTTPClient == nil {
		n.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	params := url.Values{
		"format": {"json"},
		"limit":  {"1"},
		"q":      {q},
	}
	endpoint := n.BaseURL + "/search?" + params.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if n.UserAgent != "" {
		req.Header.Set("User-Agent", n.UserAgent)
	}
	if n.Email != "" {
		req.Header.Set("From", n.Email)
	}

	resp, err := n.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var arr []nomResp
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		return nil, err
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("not found")
	}
	lat, err1 := strconvParse(arr[0].Lat)
	lon, err2 := strconvParse(arr[0].Lon)
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("bad coords")
	}
	qual := arr[0].Type
	if strings.TrimSpace(qual) == "" {
		qual = "unknown"
	}
	return &Result{Lat: lat, Lon: lon, Quality: qual, Provider: "nominatim"}, nil
}

func strconvParse(s string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
}
