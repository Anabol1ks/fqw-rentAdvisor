package dedupe

import (
	"context"
	"sort"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type row struct {
	ID          uint64
	DealType    string
	Rooms       *int
	AreaTotal   *float64
	PriceRUB    *float64
	Floor       *int
	FloorsTotal *int
	YearBuilt   *int
	HouseMat    *string `gorm:"column:house_material"`
	Lat         *float64
	Lon         *float64
	LastSeen    time.Time
}

type pair struct {
	A, B  uint64
	Score float64
}

func fetchCandidatePairs(db *gorm.DB) ([]pair, error) {
	var out []pair
	sql := `
WITH cand AS (
  SELECT l1.id AS a, l2.id AS b,
         l1.rooms AS r1, l2.rooms AS r2,
         l1.area_total AS at1, l2.area_total AS at2,
         l1.price_rub AS p1, l2.price_rub AS p2,
         l1.floor AS f1, l2.floor AS f2,
         l1.floors_total AS ft1, l2.floors_total AS ft2,
         ST_Distance(l1.geom, l2.geom) AS d
  FROM listing l1
  JOIN listing l2
    ON l1.deal_type = l2.deal_type
   AND l1.city = l2.city
   AND l1.id < l2.id
   AND l1.geom IS NOT NULL AND l2.geom IS NOT NULL
   AND ST_DWithin(l1.geom, l2.geom, 50)
  WHERE l1.is_active AND l2.is_active
)
SELECT a, b FROM cand
WHERE
  (r1 IS NULL AND r2 IS NULL OR r1 = r2)
  AND (at1 IS NOT NULL AND at2 IS NOT NULL AND abs(at1 - at2) <= 2)
  AND (
    p1 IS NULL OR p2 IS NULL OR
    (p1 > 0 AND p2 > 0 AND abs(p1 - p2)/greatest(p1,p2) <= 0.05)
  )
LIMIT 50000`
	if err := db.Raw(sql).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func loadRows(db *gorm.DB, ids []uint64) (map[uint64]row, error) {
	var rs []row
	if err := db.Raw(`
      SELECT id, deal_type, rooms, area_total, price_rub, floor, floors_total, year_built,
	      house_material, lat, lon, last_seen
      FROM listing WHERE id IN ?`, ids).Scan(&rs).Error; err != nil {
		return nil, err
	}
	m := make(map[uint64]row, len(rs))
	for _, r := range rs {
		m[r.ID] = r
	}
	return m, nil
}

func scorePair(a, b row) float64 {
	s := 0.0
	// гео уже 50м — начислим 0.4
	s += 0.40
	// площадь (±2 м² уже соблюдено) — 0.25
	if a.AreaTotal != nil && b.AreaTotal != nil {
		s += 0.25
	}
	// цена (±5% уже соблюдено) — 0.20
	if a.PriceRUB != nil && b.PriceRUB != nil {
		s += 0.20
	}
	// этаж (равенство) — 0.10
	if a.Floor != nil && b.Floor != nil && *a.Floor == *b.Floor {
		s += 0.10
	}
	// материал/год — 0.05 (грубая проверка): 0.03 за год +-1, 0.02 за похожий материал
	if a.YearBuilt != nil && b.YearBuilt != nil {
		if absInt(*a.YearBuilt-*b.YearBuilt) <= 1 {
			s += 0.03
		}
	}
	if a.HouseMat != nil && b.HouseMat != nil {
		aa := substr(*a.HouseMat, 0, 6)
		bb := substr(*b.HouseMat, 0, 6)
		if aa == bb {
			s += 0.02
		}
	}
	return s
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
func substr(s string, i, n int) string {
	if i >= len(s) {
		return ""
	}
	j := i + n
	if j > len(s) {
		j = len(s)
	}
	return s[i:j]
}

type UF struct {
	p map[uint64]uint64
	r map[uint64]int
}

func NewUF() *UF { return &UF{p: map[uint64]uint64{}, r: map[uint64]int{}} }
func (u *UF) find(x uint64) uint64 {
	if _, ok := u.p[x]; !ok {
		u.p[x] = x
		u.r[x] = 0
	}
	if u.p[x] != x {
		u.p[x] = u.find(u.p[x])
	}
	return u.p[x]
}
func (u *UF) union(a, b uint64) {
	pa, pb := u.find(a), u.find(b)
	if pa == pb {
		return
	}
	if u.r[pa] < u.r[pb] {
		pa, pb = pb, pa
	}
	u.p[pb] = pa
	if u.r[pa] == u.r[pb] {
		u.r[pa]++
	}
}

func chooseMaster(rs []row) uint64 {
	type cand struct {
		id    uint64
		score float64
	}
	cs := make([]cand, 0, len(rs))
	for _, r := range rs {
		nonNull := 0
		if r.AreaTotal != nil {
			nonNull++
		}
		if r.PriceRUB != nil {
			nonNull++
		}
		if r.Floor != nil {
			nonNull++
		}
		if r.YearBuilt != nil {
			nonNull++
		}
		if r.HouseMat != nil {
			nonNull++
		}
		if r.Lat != nil && r.Lon != nil {
			nonNull += 2
		}
		// свежесть сильнее; если нет last_seen, считаем очень старым
		ageDays := 365.0
		if !r.LastSeen.IsZero() {
			ageDays = time.Since(r.LastSeen).Hours() / 24.0
		}
		fresh := 1.0 / (1.0 + ageDays)
		sc := float64(nonNull) + 3.0*fresh
		cs = append(cs, cand{r.ID, sc})
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].score > cs[j].score })
	return cs[0].id
}

func Run(db *gorm.DB, log *zap.Logger) error {
	ctx := context.Background()
	pairs, err := fetchCandidatePairs(db)
	if err != nil {
		return err
	}
	if len(pairs) == 0 {
		log.Info("dedupe: no candidate pairs")
		return nil
	}
	// соберём уникальные ID и загрузим строки
	idset := map[uint64]struct{}{}
	for _, p := range pairs {
		idset[p.A] = struct{}{}
		idset[p.B] = struct{}{}
	}
	ids := make([]uint64, 0, len(idset))
	for id := range idset {
		ids = append(ids, id)
	}
	rows, err := loadRows(db, ids)
	if err != nil {
		return err
	}

	// посчитаем скор на парах и объединим
	uf := NewUF()
	finalPairs := make([]pair, 0, len(pairs))
	for _, p := range pairs {
		s := scorePair(rows[p.A], rows[p.B])
		if s >= 0.70 {
			finalPairs = append(finalPairs, pair{A: p.A, B: p.B, Score: s})
			uf.union(p.A, p.B)
		}
	}
	// соберём кластеры
	clusters := map[uint64][]row{} // rep -> rows
	for id := range idset {
		rep := uf.find(id)
		clusters[rep] = append(clusters[rep], rows[id])
	}

	// запишем кластеры
	tx := db.WithContext(ctx).Begin()
	defer func() {
		if tx.Error != nil {
			_ = tx.Rollback()
		}
	}()
	for _, members := range clusters {
		if len(members) < 2 {
			continue
		}
		master := chooseMaster(members)
		for _, r := range members {
			if r.ID == master {
				continue
			}
			if err := tx.Exec(`
				INSERT INTO dedupe_cluster(master_id, member_id, score, method, created_at)
				VALUES ($1,$2,$3,$4, now())
				ON CONFLICT (member_id) DO UPDATE
				SET master_id=EXCLUDED.master_id, score=EXCLUDED.score, method=EXCLUDED.method`,
				master, r.ID, 0.90, "geo50m+area2+price5+floor").Error; err != nil {
				return err
			}
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}

	log.Info("dedupe done",
		zap.Int("pairs", len(finalPairs)),
		zap.Int("clusters", len(clusters)))
	return nil
}
