package repo

import (
	"context"
	"encoding/json"
	"errors"
	"skripe/internal/model"
	"time"

	"gorm.io/gorm"
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

// UpsertListingRaw выполняет upsert. Возвращает inserted=true если была новая строка.
// Для определения вставки используем системную колонку xmax: у только что
// вставленных строк xmax = 0. Работает в PostgreSQL.
func (r *RawRepository) UpsertListingRaw(ctx context.Context, it RawItem) (bool, error) {
	payload := it.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payload["ingested_at"] = time.Now().UTC()

	// marshal в JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return false, err
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
	// В GORM нет прямого способа узнать вставлено или обновлено при upsert.
	// Обойдёмся ручным raw SQL с RETURNING и xmax.
	// Составим INSERT ... ON CONFLICT DO UPDATE ... RETURNING (xmax = 0) AS inserted;

	// Табличное имя берём из модели
	tbl := rec.TableName()
	// Используем plain SQL: перечислим нужные колонки явно (соответствует AssignmentColumns выше)
	// ВНИМАНИЕ: порядок параметров должен совпадать.

	sql := `INSERT INTO ` + tbl + ` (
		source, deal_type, external_id, url, title, description,
		price_value, price_currency, price_period,
		rooms, area_total, area_living, area_kitchen,
		floor, floors_total, year_built, house_material, condition,
		address_text, lat, lon, district, metro, payload, collected_at, created_at, updated_at
	) VALUES (
		@source, @deal_type, @external_id, @url, @title, @description,
		@price_value, @price_currency, @price_period,
		@rooms, @area_total, @area_living, @area_kitchen,
		@floor, @floors_total, @year_built, @house_material, @condition,
		@address_text, @lat, @lon, @district, @metro, @payload, @collected_at, now(), now()
	) ON CONFLICT (source, deal_type, external_id) DO UPDATE SET
		url = EXCLUDED.url,
		title = EXCLUDED.title,
		description = EXCLUDED.description,
		price_value = EXCLUDED.price_value,
		price_currency = EXCLUDED.price_currency,
		price_period = EXCLUDED.price_period,
		rooms = EXCLUDED.rooms,
		area_total = EXCLUDED.area_total,
		area_living = EXCLUDED.area_living,
		area_kitchen = EXCLUDED.area_kitchen,
		floor = EXCLUDED.floor,
		floors_total = EXCLUDED.floors_total,
		year_built = EXCLUDED.year_built,
		house_material = EXCLUDED.house_material,
		condition = EXCLUDED.condition,
		address_text = EXCLUDED.address_text,
		lat = EXCLUDED.lat,
		lon = EXCLUDED.lon,
		district = EXCLUDED.district,
		metro = EXCLUDED.metro,
		payload = EXCLUDED.payload,
		collected_at = EXCLUDED.collected_at,
		updated_at = now()
	RETURNING (xmax = 0) AS inserted;`

	var inserted bool
	// Используем named parameters через map[string]any
	params := map[string]any{
		"source":         rec.Source,
		"deal_type":      rec.DealType,
		"external_id":    rec.ExternalID,
		"url":            rec.URL,
		"title":          rec.Title,
		"description":    rec.Description,
		"price_value":    rec.PriceValue,
		"price_currency": rec.PriceCurrency,
		"price_period":   rec.PricePeriod,
		"rooms":          rec.Rooms,
		"area_total":     rec.AreaTotal,
		"area_living":    rec.AreaLiving,
		"area_kitchen":   rec.AreaKitchen,
		"floor":          rec.Floor,
		"floors_total":   rec.FloorsTotal,
		"year_built":     rec.YearBuilt,
		"house_material": rec.HouseMaterial,
		"condition":      rec.Condition,
		"address_text":   rec.AddressText,
		"lat":            rec.Lat,
		"lon":            rec.Lon,
		"district":       rec.District,
		"metro":          rec.Metro,
		"payload":        rec.Payload,
		"collected_at":   rec.CollectedAt,
	}
	if err := r.DB.WithContext(ctx).Raw(sql, params).Scan(&inserted).Error; err != nil {
		// Если вдруг база не поддерживает xmax (не PostgreSQL) — вернём ошибку
		if errors.Is(err, gorm.ErrInvalidDB) {
			return false, err
		}
		return false, err
	}
	return inserted, nil
}
