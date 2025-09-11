package normalize

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Options управляющие параметры нормализации
type Options struct {
	Limit           int
	BatchSize       int
	SleepBetween    time.Duration
	DefaultCity     string
	DailyToMonthlyK float64 // коэффициент перевода суточной аренды в месячную
}

// metro patterns: учитываем неразрывные пробелы и возможную точку после "мин"
var (
	metroRe            = regexp.MustCompile(`(?i)^\s*([^\d,;]+?)\s+(\d{1,3})\s*мин\.?`)
	metroMinutesTailRe = regexp.MustCompile(`(?i)\s+\d{1,3}\s*мин\.?`)
	nbSpaceRe          = regexp.MustCompile("[\u00A0\u202F]+")
)

type rawRow struct {
	ID            uint64
	Source        string
	DealType      string
	ExternalID    string
	URL           string
	Title         sql.NullString
	Description   sql.NullString
	PriceValue    sql.NullFloat64
	PriceCurrency sql.NullString
	PricePeriod   sql.NullString
	Rooms         sql.NullInt64
	AreaTotal     sql.NullFloat64
	AreaLiving    sql.NullFloat64
	AreaKitchen   sql.NullFloat64
	Floor         sql.NullInt64
	FloorsTotal   sql.NullInt64
	YearBuilt     sql.NullInt64
	HouseMaterial sql.NullString
	Condition     sql.NullString
	AddressText   sql.NullString
	Lat           sql.NullFloat64
	Lon           sql.NullFloat64
	District      sql.NullString
	Metro         sql.NullString
	MetroStation  sql.NullString
	MetroWalkMin  sql.NullInt64
	CollectedAt   time.Time
}

// Run выполняет чтение listing_raw и upsert в listing
func Run(db *gorm.DB, log *zap.Logger, o Options) error {
	if o.BatchSize <= 0 {
		o.BatchSize = 500
	}
	ctx := context.Background()
	processed := 0
	inserted := 0
	updated := 0
	lastID := uint64(0)

	for {
		// выбираем батч raw по возрастанию id (не фильтруем пока по состоянию)
		var rows []rawRow
		tx := db.WithContext(ctx).Raw(`SELECT id, source, deal_type, external_id, url, title, description, price_value, price_currency, price_period, rooms, area_total, area_living, area_kitchen, floor, floors_total, year_built, house_material, condition, address_text, lat, lon, district, metro, metro_station, metro_walk_min, collected_at FROM listing_raw WHERE id > ? ORDER BY id LIMIT ?`, lastID, o.BatchSize).Scan(&rows)
		if tx.Error != nil {
			return tx.Error
		}
		if len(rows) == 0 {
			break
		}

		for _, r := range rows {
			if o.Limit > 0 && processed >= o.Limit {
				break
			}
			pr, _ := computePrice(r.DealType, r.PriceValue, r.PricePeriod, o.DailyToMonthlyK)
			if pr != nil && (r.AreaTotal.Valid && r.AreaTotal.Float64 > 0) {
				ppm := *pr / r.AreaTotal.Float64
				pround := math.Round(ppm*100) / 100
				// continue with upsert including price_per_m2
				// parse metro
				station, walk := parseMetro(r.Metro.String)
				if station == "" && r.MetroStation.Valid {
					station = r.MetroStation.String
				}
				var walkPtr *int
				if walk > 0 {
					walkPtr = &walk
				}
				// validation
				if r.Floor.Valid && r.FloorsTotal.Valid && r.Floor.Int64 > r.FloorsTotal.Int64 {
					// пропускаем абсурд
					log.Debug("skip inconsistent floor", zap.Uint64("id", r.ID))
					processed++
					continue
				}
				if r.AreaLiving.Valid && r.AreaTotal.Valid && r.AreaLiving.Float64 > r.AreaTotal.Float64 {
					r.AreaLiving.Valid = false
				}

				addrNorm := strings.TrimSpace(r.AddressText.String)
				city := o.DefaultCity
				district := nullToString(r.District)
				now := time.Now().UTC()

				// upsert через raw SQL, сохраняя first_seen только при вставке
				sql := `INSERT INTO listing
                (source, deal_type, external_id, url, title, description, price_rub, price_period, price_per_m2, rooms, area_total, area_living, area_kitchen, floor, floors_total, year_built, house_material, condition, address_norm, city, district, metro, metro_station, metro_walk_min, contact_phone_hash, is_active, first_seen, last_seen, lat, lon)
                VALUES (@source,@deal_type,@external_id,@url,@title,@description,@price_rub,@price_period,@price_per_m2,@rooms,@area_total,@area_living,@area_kitchen,@floor,@floors_total,@year_built,@house_material,@condition,@address_norm,@city,@district,@metro,@metro_station,@metro_walk_min,NULL,true,@first_seen,@last_seen,@lat,@lon)
                ON CONFLICT (source, deal_type, external_id) DO UPDATE SET
                  url=EXCLUDED.url,
                  title=EXCLUDED.title,
                  description=EXCLUDED.description,
                  price_rub=EXCLUDED.price_rub,
                  price_period=EXCLUDED.price_period,
                  price_per_m2=EXCLUDED.price_per_m2,
                  rooms=EXCLUDED.rooms,
                  area_total=EXCLUDED.area_total,
                  area_living=EXCLUDED.area_living,
                  area_kitchen=EXCLUDED.area_kitchen,
                  floor=EXCLUDED.floor,
                  floors_total=EXCLUDED.floors_total,
                  year_built=EXCLUDED.year_built,
                  house_material=EXCLUDED.house_material,
                  condition=EXCLUDED.condition,
                  address_norm=EXCLUDED.address_norm,
                  city=EXCLUDED.city,
                  district=EXCLUDED.district,
                  metro=EXCLUDED.metro,
                  metro_station=EXCLUDED.metro_station,
                  metro_walk_min=EXCLUDED.metro_walk_min,
                  is_active=true,
                  last_seen=EXCLUDED.last_seen,
                  lat=EXCLUDED.lat,
                  lon=EXCLUDED.lon
                RETURNING (xmax = 0) AS inserted`

				params := map[string]any{
					"source":         r.Source,
					"deal_type":      r.DealType,
					"external_id":    r.ExternalID,
					"url":            r.URL,
					"title":          nullToPtr(r.Title),
					"description":    nullToPtr(r.Description),
					"price_rub":      pr,
					"price_period":   nullToPtr(r.PricePeriod),
					"price_per_m2":   pround,
					"rooms":          nullIntPtr(r.Rooms),
					"area_total":     nullFloatPtr(r.AreaTotal),
					"area_living":    nullFloatPtr(r.AreaLiving),
					"area_kitchen":   nullFloatPtr(r.AreaKitchen),
					"floor":          nullIntPtr(r.Floor),
					"floors_total":   nullIntPtr(r.FloorsTotal),
					"year_built":     nullIntPtr(r.YearBuilt),
					"house_material": nullToPtr(r.HouseMaterial),
					"condition":      nullToPtr(r.Condition),
					"address_norm":   addrNorm,
					"city":           city,
					"district":       district,
					"metro":          nullToPtr(r.Metro),
					"metro_station":  station,
					"metro_walk_min": walkPtr,
					"first_seen":     r.CollectedAt,
					"last_seen":      now,
					"lat":            nullFloatPtr(r.Lat),
					"lon":            nullFloatPtr(r.Lon),
				}
				var ins bool
				if err := db.WithContext(ctx).Raw(sql, params).Scan(&ins).Error; err != nil {
					log.Warn("upsert listing", zap.Error(err), zap.String("ext_id", r.ExternalID))
				} else {
					if ins {
						inserted++
					} else {
						updated++
					}
				}
			} else {
				// Нет цены или площади — пропускаем расчёт ppm
				// но всё равно создаём listing без price_per_m2
				station, walk := parseMetro(r.Metro.String)
				if station == "" && r.MetroStation.Valid {
					station = r.MetroStation.String
				}
				var walkPtr *int
				if walk > 0 {
					walkPtr = &walk
				}
				addrNorm := strings.TrimSpace(r.AddressText.String)
				city := o.DefaultCity
				district := nullToString(r.District)
				now := time.Now().UTC()
				pr, _ := computePrice(r.DealType, r.PriceValue, r.PricePeriod, o.DailyToMonthlyK)
				sql := `INSERT INTO listing
                (source, deal_type, external_id, url, title, description, price_rub, price_period, price_per_m2, rooms, area_total, area_living, area_kitchen, floor, floors_total, year_built, house_material, condition, address_norm, city, district, metro, metro_station, metro_walk_min, contact_phone_hash, is_active, first_seen, last_seen, lat, lon)
                VALUES (@source,@deal_type,@external_id,@url,@title,@description,@price_rub,@price_period,NULL,@rooms,@area_total,@area_living,@area_kitchen,@floor,@floors_total,@year_built,@house_material,@condition,@address_norm,@city,@district,@metro,@metro_station,@metro_walk_min,NULL,true,@first_seen,@last_seen,@lat,@lon)
                ON CONFLICT (source, deal_type, external_id) DO UPDATE SET
                  url=EXCLUDED.url,
                  title=EXCLUDED.title,
                  description=EXCLUDED.description,
                  price_rub=EXCLUDED.price_rub,
                  price_period=EXCLUDED.price_period,
                  price_per_m2=EXCLUDED.price_per_m2,
                  rooms=EXCLUDED.rooms,
                  area_total=EXCLUDED.area_total,
                  area_living=EXCLUDED.area_living,
                  area_kitchen=EXCLUDED.area_kitchen,
                  floor=EXCLUDED.floor,
                  floors_total=EXCLUDED.floors_total,
                  year_built=EXCLUDED.year_built,
                  house_material=EXCLUDED.house_material,
                  condition=EXCLUDED.condition,
                  address_norm=EXCLUDED.address_norm,
                  city=EXCLUDED.city,
                  district=EXCLUDED.district,
                  metro=EXCLUDED.metro,
                  metro_station=EXCLUDED.metro_station,
                  metro_walk_min=EXCLUDED.metro_walk_min,
                  is_active=true,
                  last_seen=EXCLUDED.last_seen,
                  lat=EXCLUDED.lat,
                  lon=EXCLUDED.lon
                RETURNING (xmax = 0) AS inserted`
				params := map[string]any{
					"source":         r.Source,
					"deal_type":      r.DealType,
					"external_id":    r.ExternalID,
					"url":            r.URL,
					"title":          nullToPtr(r.Title),
					"description":    nullToPtr(r.Description),
					"price_rub":      pr,
					"price_period":   nullToPtr(r.PricePeriod),
					"rooms":          nullIntPtr(r.Rooms),
					"area_total":     nullFloatPtr(r.AreaTotal),
					"area_living":    nullFloatPtr(r.AreaLiving),
					"area_kitchen":   nullFloatPtr(r.AreaKitchen),
					"floor":          nullIntPtr(r.Floor),
					"floors_total":   nullIntPtr(r.FloorsTotal),
					"year_built":     nullIntPtr(r.YearBuilt),
					"house_material": nullToPtr(r.HouseMaterial),
					"condition":      nullToPtr(r.Condition),
					"address_norm":   addrNorm,
					"city":           city,
					"district":       district,
					"metro":          nullToPtr(r.Metro),
					"metro_station":  station,
					"metro_walk_min": walkPtr,
					"first_seen":     r.CollectedAt,
					"last_seen":      now,
					"lat":            nullFloatPtr(r.Lat),
					"lon":            nullFloatPtr(r.Lon),
				}
				var ins bool
				if err := db.WithContext(ctx).Raw(sql, params).Scan(&ins).Error; err != nil {
					log.Warn("upsert listing (nopm2)", zap.Error(err), zap.String("ext_id", r.ExternalID))
				} else {
					if ins {
						inserted++
					} else {
						updated++
					}
				}
			}
			processed++
			if r.ID > lastID {
				lastID = r.ID
			}
			if o.Limit > 0 && processed >= o.Limit {
				break
			}
		}

		log.Info("batch done", zap.Int("processed", processed), zap.Int("inserted", inserted), zap.Int("updated", updated))
		if o.Limit > 0 && processed >= o.Limit {
			break
		}
		if o.SleepBetween > 0 {
			time.Sleep(o.SleepBetween)
		}
	}

	log.Info("normalize finished", zap.Int("processed", processed), zap.Int("inserted", inserted), zap.Int("updated", updated))
	return nil
}

func computePrice(dealType string, priceValue sql.NullFloat64, pricePeriod sql.NullString, dailyK float64) (*float64, *string) {
	if !priceValue.Valid {
		return nil, nil
	}
	v := priceValue.Float64
	var period *string
	switch dealType {
	case "sale":
		return &v, period
	case "rent_long":
		// ожидаем месячную цену
		p := "month"
		period = &p
		return &v, period
	case "rent_daily":
		// конвертируем в месячную
		if dailyK <= 0 {
			dailyK = 30
		}
		m := v * dailyK
		p := "month"
		period = &p
		return &m, period
	default:
		return &v, period
	}
}

func parseMetro(raw string) (station string, walkMin int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0
	}
	// нормализуем неразрывные пробелы
	raw = nbSpaceRe.ReplaceAllString(raw, " ")
	// Берём первую часть до запятой
	parts := strings.Split(raw, ",")
	candidate := strings.TrimSpace(parts[0])
	m := metroRe.FindStringSubmatch(candidate)
	if len(m) == 3 {
		st := strings.TrimSpace(m[1])
		st = strings.Trim(st, "-—– ")
		station = st
		fmt.Sscanf(m[2], "%d", &walkMin)
	} else {
		// срежем возможный " 8 мин." хвост, если regex не сработал из-за символов
		cleaned := metroMinutesTailRe.ReplaceAllString(candidate, "")
		station = strings.TrimSpace(cleaned)
	}
	return
}

func nullToPtr(ns sql.NullString) *string {
	if ns.Valid {
		s := ns.String
		return &s
	}
	return nil
}
func nullToString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
func nullIntPtr(n sql.NullInt64) *int {
	if n.Valid {
		v := int(n.Int64)
		return &v
	}
	return nil
}
func nullFloatPtr(n sql.NullFloat64) *float64 {
	if n.Valid {
		v := n.Float64
		return &v
	}
	return nil
}

// defensive helper (not used yet)
var ErrSkip = errors.New("skip")
