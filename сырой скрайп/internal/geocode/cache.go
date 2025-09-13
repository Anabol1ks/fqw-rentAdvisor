package geocode

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"time"

	"gorm.io/gorm"
)

type cacheRow struct {
	AddressHash string
	AddressNorm string
	Lat         *float64
	Lon         *float64
	Quality     string
	Provider    string
	CreatedAt   time.Time
}

func addrHash(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func getCache(ctx context.Context, db *gorm.DB, address string) (*cacheRow, error) {
	var row cacheRow
	err := db.WithContext(ctx).Table("geocode_cache").
		Where("address_hash = ?", addrHash(address)).Take(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func putCache(ctx context.Context, db *gorm.DB, address string, r *Result) error {
	return db.WithContext(ctx).Exec(`
		INSERT INTO geocode_cache(address_hash, address_norm, lat, lon, quality, provider, created_at)
		VALUES ($1,$2,$3,$4,$5,$6, now())
		ON CONFLICT (address_hash) DO UPDATE
		SET address_norm=EXCLUDED.address_norm, lat=EXCLUDED.lat, lon=EXCLUDED.lon,
		    quality=EXCLUDED.quality, provider=EXCLUDED.provider`,
		addrHash(address), address, r.Lat, r.Lon, r.Quality, r.Provider).Error
}
