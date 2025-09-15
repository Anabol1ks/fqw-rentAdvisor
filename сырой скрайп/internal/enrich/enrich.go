package enrich

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// RunCity запускает обогащение по одному городу (если city == "", то по всем)
func RunCity(db *gorm.DB, log *zap.Logger, city string) error {
	ctx := context.Background()

	// quick sanity: check ref_metro presence
	var metroCnt int64
	if city == "" {
		_ = db.WithContext(ctx).Table("ref_metro").Count(&metroCnt).Error
	} else {
		_ = db.WithContext(ctx).Table("ref_metro").Where("city = ?", city).Count(&metroCnt).Error
	}
	if metroCnt == 0 {
		// Доп. проверка: возможно, проблема в кодировке флага --city под Windows (mojibake).
		// Если ref_metro в принципе не пуст, но по конкретному city записей 0 —
		// снимаем фильтр и выполняем обогащение для всех городов, чтобы не оставлять всё пустым.
		var totalMetro int64
		_ = db.WithContext(ctx).Table("ref_metro").Count(&totalMetro).Error
		if city != "" && totalMetro > 0 {
			log.Warn("no metro stations matched for provided city; running without city filter (check console encoding / --city)", zap.String("city", city))
			city = "" // сбрасываем фильтр
		} else {
			if city == "" {
				log.Warn("ref_metro is empty — dist_to_metro_m won't be computed")
			} else {
				log.Warn("no metro stations for city — dist_to_metro_m won't be computed", zap.String("city", city))
			}
		}
	}

	// 2.2. Расстояние до ближайшего метро + подстановка metro_station если пусто
	qNearest := `
WITH nearest AS (
  SELECT DISTINCT ON (l.id)
         l.id,
         m.name,
         ST_Distance(l.geom, m.geom) AS dist_m
  FROM listing l
  JOIN ref_metro m ON m.city = l.city
	WHERE l.geom IS NOT NULL AND l.dist_to_metro_m IS NULL %s
  ORDER BY l.id, l.geom <-> m.geom
)
UPDATE listing AS l
SET dist_to_metro_m = n.dist_m,
    metro_station   = COALESCE(l.metro_station, n.name)
FROM nearest n
WHERE l.id = n.id;`
	cond := ""
	args := []any{}
	if city != "" {
		cond = "AND l.city = ?"
		args = append(args, city)
	}
	res := db.WithContext(ctx).Exec(fmt.Sprintf(qNearest, cond), args...)
	if res.Error != nil {
		return res.Error
	}
	log.Info("enrich: nearest metro updated", zap.Int64("rows", res.RowsAffected))

	// 2.3. Расстояние до центра
	qCenter := `
UPDATE listing AS l
SET dist_to_center_km = ST_Distance(l.geom, c.center) / 1000.0
FROM ref_city c
WHERE l.city = c.city AND l.geom IS NOT NULL AND l.dist_to_center_km IS NULL %s;`
	cond2 := ""
	args2 := []any{}
	if city != "" {
		cond2 = "AND l.city = ?"
		args2 = append(args2, city)
	}
	res2 := db.WithContext(ctx).Exec(fmt.Sprintf(qCenter, cond2), args2...)
	if res2.Error != nil {
		return res2.Error
	}
	log.Info("enrich: center distance updated", zap.Int64("rows", res2.RowsAffected))

	// 2.4. Плотность предложений в 500 м
	qDensity := `
WITH cnt AS (
  SELECT l1.id,
         COUNT(*) - 1 AS neighbors_500m
  FROM listing l1
  JOIN listing l2
    ON l2.is_active
   AND l2.geom IS NOT NULL
   AND ST_DWithin(l1.geom, l2.geom, 500)
	WHERE l1.geom IS NOT NULL AND l1.density_500m IS NULL %s
  GROUP BY l1.id
)
UPDATE listing AS l
SET density_500m = c.neighbors_500m
FROM cnt c
WHERE l.id = c.id;`
	cond3 := ""
	args3 := []any{}
	if city != "" {
		cond3 = "AND l1.city = ? AND l2.city = ?"
		args3 = append(args3, city, city)
	}
	res3 := db.WithContext(ctx).Exec(fmt.Sprintf(qDensity, cond3), args3...)
	if res3.Error != nil {
		return res3.Error
	}
	log.Info("enrich: density updated", zap.Int64("rows", res3.RowsAffected))

	log.Info("enrich done", zap.String("city", city))
	return nil
}
