package geocode

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Options struct {
	Workers     int
	Limit       int
	Sleep       time.Duration
	DefaultCity string
}

type listingRow struct {
	ID          uint64
	AddressNorm string
	City        string
	Lat         *float64
	Lon         *float64
}

func BuildQuery(addrNorm, city, defCity string) string {
	a := strings.TrimSpace(addrNorm)
	if a == "" {
		return strings.TrimSpace(city)
	}
	// если в адресе нет города — допишем
	if !strings.Contains(a, city) && defCity != "" && !strings.Contains(a, defCity) {
		if city != "" {
			return city + ", " + a
		}
		return defCity + ", " + a
	}
	return a
}

func RunBatch(db *gorm.DB, log *zap.Logger, prov Provider, o Options) error {
	if o.Workers <= 0 {
		o.Workers = 2
	}

	ctx := context.Background()
	var lastID uint64
	processed := 0
	okCount := 0

	for {
		// Берём порцию без координат, но с адресом
		var rows []listingRow
		if err := db.WithContext(ctx).Raw(`
			SELECT id, address_norm, city, lat, lon
			FROM listing
			WHERE (lat IS NULL OR lon IS NULL) AND address_norm IS NOT NULL
			  AND id > ?
			ORDER BY id
			LIMIT 500`, lastID).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}

		type job struct{ row listingRow }
		jobs := make(chan job, len(rows))
		var wg sync.WaitGroup
		wg.Add(o.Workers)

		for w := 0; w < o.Workers; w++ {
			go func() {
				defer wg.Done()
				for j := range jobs {
					if o.Limit > 0 && processed >= o.Limit {
						continue
					}
					addr := BuildQuery(j.row.AddressNorm, j.row.City, o.DefaultCity)
					// КЭШ
					if cr, err := getCache(ctx, db, addr); err == nil && cr != nil && cr.Lat != nil && cr.Lon != nil {
						// сразу апдейт listing
						_ = db.WithContext(ctx).Exec(`UPDATE listing SET lat=$1, lon=$2 WHERE id=$3`,
							*cr.Lat, *cr.Lon, j.row.ID).Error
						okCount++
						continue
					}
					// Провайдер
					res, err := prov.Geocode(ctx, addr)
					if err != nil {
						log.Warn("geocode fail", zap.String("addr", addr), zap.Error(err))
						continue
					}
					// Кэш + апдейт
					_ = putCache(ctx, db, addr, res)
					_ = db.WithContext(ctx).Exec(`UPDATE listing SET lat=$1, lon=$2 WHERE id=$3`,
						res.Lat, res.Lon, j.row.ID).Error
					okCount++
					if o.Sleep > 0 {
						time.Sleep(o.Sleep)
					}
				}
			}()
		}

		for _, r := range rows {
			if r.ID > lastID {
				lastID = r.ID
			}
			if o.Limit > 0 && processed >= o.Limit {
				break
			}
			jobs <- job{row: r}
			processed++
		}
		close(jobs)
		wg.Wait()

		log.Info("geocode batch", zap.Int("processed", processed), zap.Int("geocoded", okCount))
		if o.Limit > 0 && processed >= o.Limit {
			break
		}
	}

	log.Info("geocode done", zap.Int("processed", processed), zap.Int("geocoded", okCount))
	return nil
}
