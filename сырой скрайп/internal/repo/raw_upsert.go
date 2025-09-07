package repo

import (
	"context"
	"encoding/json"
	"skripe/internal/model"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RawItem struct {
	Source        string
	DealType      string
	ExternalID    string
	URL           string
	Title         string
	Description   string
	PriceValue    *float64
	PriceCurrency string
	Rooms         *int
	AreaTotal     *float64
	AreaLiving    *float64
	AreaKitchen   *float64
	Floor         *int
	FloorsTotal   *int
	YearBuilt     *int
	HouseMaterial string
	Condition     string
	AddressText   string
	Lat           *float64
	Lon           *float64
	District      string
	Metro         string
	PricePeriod   *string
	Payload       map[string]any
}

type RawRepository struct{ DB *gorm.DB }

func NewRawRepository(db *gorm.DB) *RawRepository { return &RawRepository{DB: db} }

func (r *RawRepository) UpsertListingRaw(ctx context.Context, it RawItem) error {
	payload := it.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payload["ingested_at"] = time.Now().UTC()

	// marshal в JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	rec := model.ListingRaw{
		Source:        it.Source,
		DealType:      it.DealType,
		ExternalID:    it.ExternalID,
		URL:           it.URL,
		Title:         it.Title,
		Description:   it.Description,
		PriceValue:    it.PriceValue,
		PriceCurrency: it.PriceCurrency,
		PricePeriod:   it.PricePeriod,
		Rooms:         it.Rooms,
		AreaTotal:     it.AreaTotal,
		AreaLiving:    it.AreaLiving,
		AreaKitchen:   it.AreaKitchen,
		Floor:         it.Floor,
		FloorsTotal:   it.FloorsTotal,
		YearBuilt:     it.YearBuilt,
		HouseMaterial: it.HouseMaterial,
		Condition:     it.Condition,
		AddressText:   it.AddressText,
		Lat:           it.Lat,
		Lon:           it.Lon,
		District:      it.District,
		Metro:         it.Metro,
		Payload:       payloadBytes,
		CollectedAt:   time.Now().UTC(),
	}

	return r.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "source"},
				{Name: "deal_type"},
				{Name: "external_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"deal_type", "url", "title", "description", "price_value", "price_currency",
				"price_period",
				"rooms", "area_total", "area_living", "area_kitchen",
				"floor", "floors_total", "year_built", "house_material", "condition",
				"address_text", "lat", "lon", "district", "metro", "payload", "collected_at",
			}),
		}).
		Create(&rec).Error
}
