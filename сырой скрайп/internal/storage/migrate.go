package storage

import (
	"skripe/internal/model"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB, log *zap.Logger) {
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS postgis;`).Error; err != nil {
		log.Error("Ошибка PostGis: ", zap.Error(err))
	}
	if err := db.AutoMigrate(
		&model.ListingRaw{},
		&model.Listing{},
		&model.GeocodeCache{},
		&model.PriceHistory{},
	); err != nil {
		log.Error("Ошибка миграции таблиц", zap.Error(err))
	}

	if err := db.Exec(sql).Error; err != nil {
		log.Error("post-indexes: ", zap.Error(err))
	}

	if err := db.Exec(geomTrig).Error; err != nil {
		log.Error("geom trigger: ", zap.Error(err))
	}

	if err := db.Exec(priceHistoryTrig).Error; err != nil {
		log.Error("price history trigger: ", zap.Error(err))
	}

	log.Info("✅ Миграция прошла усмешно: PostGIS, tables, indexes, triggers готовы.")
}

var sql = `
-- география point для listing
ALTER TABLE listing
  ADD COLUMN IF NOT EXISTS geom geography(POINT,4326);

-- GiST индекс по геометрии
CREATE INDEX IF NOT EXISTS idx_listing_geom ON listing USING GIST (geom);

-- текстовый GIN индекс для full-text поиска по title+description
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_listing_text'
  ) THEN
    EXECUTE 'CREATE INDEX idx_listing_text ON listing USING GIN (to_tsvector(''russian'', coalesce(title,'''') || '' '' || coalesce(description, '''')));';
  END IF;
END$$;

-- индексы по городу/району и цене
CREATE INDEX IF NOT EXISTS idx_listing_city_district ON listing (city, district);
CREATE INDEX IF NOT EXISTS idx_listing_price ON listing (price_rub);

-- уникальный индекс под ключ конфликта upsert'а
CREATE UNIQUE INDEX IF NOT EXISTS uidx_listing_src_deal_ext
  ON listing(source, deal_type, external_id);

-- индексы для geocode & поиска пропусков координат
CREATE INDEX IF NOT EXISTS idx_geocode_cache_created_at ON geocode_cache(created_at);
CREATE INDEX IF NOT EXISTS idx_listing_lat_lon_null ON listing ((lat IS NULL), (lon IS NULL));
`

var geomTrig = `
CREATE OR REPLACE FUNCTION set_listing_geom() RETURNS trigger AS $$
BEGIN
  IF NEW.lat IS NOT NULL AND NEW.lon IS NOT NULL THEN
    NEW.geom := ST_SetSRID(ST_MakePoint(NEW.lon, NEW.lat), 4326)::geography;
  ELSE
    NEW.geom := NULL;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tr_set_listing_geom ON listing;
CREATE TRIGGER tr_set_listing_geom
BEFORE INSERT OR UPDATE OF lat, lon ON listing
FOR EACH ROW EXECUTE FUNCTION set_listing_geom();
`

var priceHistoryTrig = `
CREATE OR REPLACE FUNCTION log_price_change() RETURNS trigger AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.price_rub IS NOT NULL THEN
      INSERT INTO price_history(listing_id, ts, price_rub)
      VALUES (NEW.id, now(), NEW.price_rub);
    END IF;
    RETURN NEW;
  ELSIF TG_OP = 'UPDATE' THEN
    IF NEW.price_rub IS DISTINCT FROM OLD.price_rub THEN
      INSERT INTO price_history(listing_id, ts, price_rub)
      VALUES (NEW.id, now(), NEW.price_rub);
    END IF;
    RETURN NEW;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tr_log_price_change ON listing;
CREATE TRIGGER tr_log_price_change
AFTER INSERT OR UPDATE OF price_rub ON listing
FOR EACH ROW EXECUTE FUNCTION log_price_change();
`
