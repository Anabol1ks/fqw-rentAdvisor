package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type ValuationReport struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	City     string `json:"city"`
	Address  string `json:"address"`
	DealType string `json:"deal_type"`

	PredictionRub   float64 `json:"prediction_rub"`
	IntervalLowRub  float64 `json:"interval_low_rub"`
	IntervalHighRub float64 `json:"interval_high_rub"`

	RawReport datatypes.JSON `gorm:"type:jsonb" json:"raw_report"`
}
