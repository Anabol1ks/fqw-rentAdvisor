package mlclient

// То, что мы отправляем в Python /v1/predict/address
// PredictAddressRequest — запрос к ML-сервису
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
	WithText      bool     `json:"with_text"`
}

// Report — полный ответ от ML-сервиса
type Report struct {
	Object      Object       `json:"object"`
	Enriched    Enriched     `json:"enriched"`
	Pricing     Pricing      `json:"pricing"`
	Explanation Explanation  `json:"explanation"`
	Text        *TextBlocks  `json:"text,omitempty"`
	Comparables []Comparable `json:"comparables,omitempty"`
}

type Object struct {
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
	DealType      string   `json:"deal_type"`
}

type Enriched struct {
	Lat            float64  `json:"lat"`
	Lon            float64  `json:"lon"`
	DistToMetroM   *float64 `json:"dist_to_metro_m,omitempty"`
	DistToCenterKm *float64 `json:"dist_to_center_km,omitempty"`
	Density500m    *float64 `json:"density_500m,omitempty"`
	MetroWalkMin   *float64 `json:"metro_walk_min,omitempty"`
	MetroStation   *string  `json:"metro_station,omitempty"`
	District       *string  `json:"district,omitempty"`
	H37            *string  `json:"h3_7,omitempty"`
}

type Pricing struct {
	PredictionRub   float64 `json:"prediction_rub"`
	IntervalLowRub  float64 `json:"interval_low_rub"`
	IntervalHighRub float64 `json:"interval_high_rub"`
	Currency        string  `json:"currency"`
	DealType        string  `json:"deal_type"`
}

type Explanation struct {
	ModelName         string   `json:"model_name"`
	UsedLocalStats    bool     `json:"used_local_stats"`
	LocalStatsSummary *string  `json:"local_stats_summary,omitempty"`
	DistanceToClosest *float64 `json:"distance_to_closest_km,omitempty"`
}

type TextBlocks struct {
	SummaryShort   string   `json:"summary_short"`
	SummaryLong    string   `json:"summary_long"`
	FactorsSummary []string `json:"factors_summary"`
}

type Comparable struct {
	PriceRub     float64  `json:"price_rub"`
	Rooms        *int     `json:"rooms,omitempty"`
	AreaTotal    *float64 `json:"area_total,omitempty"`
	Floor        *float64 `json:"floor,omitempty"`        // <-- ИЗМЕНЕНО: int → float64
	FloorsTotal  *float64 `json:"floors_total,omitempty"` // <-- ИЗМЕНЕНО: int → float64
	YearBuilt    *float64 `json:"year_built,omitempty"`   // <-- ИЗМЕНЕНО: int → float64
	DistanceKm   *float64 `json:"distance_km,omitempty"`
	MetroStation *string  `json:"metro_station,omitempty"`
	Address      *string  `json:"address,omitempty"`
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
