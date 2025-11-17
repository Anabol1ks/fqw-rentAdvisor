package mlclient

import "time"

// То, что мы отправляем в Python /v1/predict/address
type PredictAddressRequest struct {
	City          string   `json:"city"`
	Address       string   `json:"address"`
	Rooms         int      `json:"rooms"`
	AreaTotal     float64  `json:"area_total"`
	AreaLiving    *float64 `json:"area_living,omitempty"`
	AreaKitchen   *float64 `json:"area_kitchen,omitempty"`
	Floor         *int     `json:"floor,omitempty"`
	FloorsTotal   *int     `json:"floors_total,omitempty"`
	YearBuilt     *int     `json:"year_built,omitempty"`
	HouseMaterial *string  `json:"house_material,omitempty"`
	Condition     *string  `json:"condition,omitempty"`
	WithText      bool     `json:"with_text"` // хотим сразу текст от LLM
}

// Ниже — структуры под JSON-отчёт Python-сервиса (realval_report_v1)

type Report struct {
	ReportID    string         `json:"report_id"`
	GeneratedAt time.Time      `json:"generated_at"`
	Version     string         `json:"version"`
	Object      ReportObject   `json:"object"`
	Enriched    ReportEnriched `json:"enriched"`
	Pricing     Pricing        `json:"pricing"`
	Comparables []Comparable   `json:"comparables"`
	Explanation Explanation    `json:"explanation"`
	ModelInfo   ModelInfo      `json:"model_info"`
	Text        *TextBlocks    `json:"text,omitempty"`
}

type ReportObject struct {
	Address       string   `json:"address"`
	City          string   `json:"city"`
	Rooms         int      `json:"rooms"`
	AreaTotal     float64  `json:"area_total"`
	AreaLiving    *float64 `json:"area_living"`
	AreaKitchen   *float64 `json:"area_kitchen"`
	Floor         *int     `json:"floor"`
	FloorsTotal   *int     `json:"floors_total"`
	YearBuilt     *int     `json:"year_built"`
	HouseMaterial *string  `json:"house_material"`
	Condition     *string  `json:"condition"`
}

type ReportEnriched struct {
	Lat            *float64 `json:"lat"`
	Lon            *float64 `json:"lon"`
	DistToCenterKm *float64 `json:"dist_to_center_km"`
	MetroStation   *string  `json:"metro_station"`
	DistToMetroM   *float64 `json:"dist_to_metro_m"`
	MetroWalkMin   *int     `json:"metro_walk_min"`
	Density500m    *float64 `json:"density_500m"`
}

type Pricing struct {
	PredictionRub   float64 `json:"prediction_rub"`
	IntervalLowRub  float64 `json:"interval_low_rub"`
	IntervalHighRub float64 `json:"interval_high_rub"`
	Currency        string  `json:"currency"`
	DealType        string  `json:"deal_type"`
}

type Comparable struct {
	DealType       string   `json:"deal_type"`
	City           string   `json:"city"`
	District       string   `json:"district"`
	PriceRub       float64  `json:"price_rub"`
	PricePerM2     float64  `json:"price_per_m2"`
	Rooms          *int     `json:"rooms"`
	AreaTotal      *float64 `json:"area_total"`
	AreaLiving     *float64 `json:"area_living"`
	AreaKitchen    *float64 `json:"area_kitchen"`
	Floor          *int     `json:"floor"`
	FloorsTotal    *int     `json:"floors_total"`
	YearBuilt      *int     `json:"year_built"`
	HouseMaterial  *string  `json:"house_material"`
	Condition      *string  `json:"condition"`
	Lat            *float64 `json:"lat"`
	Lon            *float64 `json:"lon"`
	DistToMetroM   *float64 `json:"dist_to_metro_m"`
	DistToCenterKm *float64 `json:"dist_to_center_km"`
	Density500m    *float64 `json:"density_500m"`
	MetroStation   *string  `json:"metro_station"`
	MetroWalkMin   *int     `json:"metro_walk_min"`
	DistanceKm     *float64 `json:"distance_km"`
}

type Explanation struct {
	IsLogSpace         bool                  `json:"is_log_space"`
	BaseValue          float64               `json:"base_value"`
	PredictionInternal float64               `json:"prediction_internal"`
	TopFeatures        []FeatureContribution `json:"top_features"`
}

type FeatureContribution struct {
	Feature         string  `json:"feature"`
	Contribution    float64 `json:"contribution"`
	AbsContribution float64 `json:"abs_contribution"`
}

type ModelInfo struct {
	ModelName string   `json:"model_name"`
	Target    string   `json:"target"`
	LogTarget bool     `json:"log_target"`
	ValidMAE  *float64 `json:"valid_mae"`
	ValidRMSE *float64 `json:"valid_rmse"`
}

type TextBlocks struct {
	SummaryShort   string   `json:"summary_short"`
	SummaryLong    string   `json:"summary_long"`
	FactorsSummary []string `json:"factors_summary"`
}
