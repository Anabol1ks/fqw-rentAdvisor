package geocode

import "context"

type Result struct {
	Lat, Lon float64
	Quality  string // "house" | "street" | "locality" | ...
	Provider string // "nominatim" | "dadata" | ...
}

type Provider interface {
	Geocode(ctx context.Context, query string) (*Result, error)
}
