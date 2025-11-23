package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Anabol1ks/rentAdvisor-utils-go/logger"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"skripe/config"
	"skripe/internal/storage"
)

type colMap struct {
	name int
	lat  int
	lon  int
	city int // optional; if -1, use --city flag
}

func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", ".")
	return strconv.ParseFloat(s, 64)
}

func detectHeader(rec []string) bool {
	if len(rec) < 3 {
		return false
	}
	joined := strings.ToLower(strings.Join(rec, ","))
	return strings.Contains(joined, "name") || strings.Contains(joined, "lat") || strings.Contains(joined, "lon") || strings.Contains(joined, "city")
}

func buildColMap(header []string) (colMap, error) {
	m := colMap{name: -1, lat: -1, lon: -1, city: -1}
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(h))
		switch key {
		case "name", "station", "metro", "title":
			if m.name == -1 {
				m.name = i
			}
		case "lat", "latitude":
			if m.lat == -1 {
				m.lat = i
			}
		case "lon", "lng", "long", "longitude":
			if m.lon == -1 {
				m.lon = i
			}
		case "city":
			if m.city == -1 {
				m.city = i
			}
		}
	}
	if m.name == -1 || m.lat == -1 || m.lon == -1 {
		return m, fmt.Errorf("header must include columns for name, lat, lon (got: %v)", header)
	}
	return m, nil
}

func main() {
	_ = godotenv.Load()
	_ = logger.Init(true)
	defer logger.Sync()
	log := logger.L()

	file := flag.String("file", "", "Путь к CSV-файлу со станциями метро")
	city := flag.String("city", "Москва", "Город по умолчанию, если нет колонки city в CSV")
	sep := flag.String("sep", ",", "Разделитель CSV (по умолчанию ',')")
	dry := flag.Bool("dry-run", false, "Не писать в БД, только парсить и подсчитывать")
	flag.Parse()

	if *file == "" {
		log.Fatal("нужно указать --file путь к CSV")
		return
	}
	if *sep == "" {
		*sep = ","
	}

	f, err := os.Open(*file)
	if err != nil {
		log.Fatal("не удалось открыть файл", zap.Error(err), zap.String("file", *file))
		return
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comma = rune((*sep)[0])
	r.FieldsPerRecord = -1

	// read first record to decide header/no header
	first, err := r.Read()
	if err != nil {
		log.Fatal("не удалось прочитать CSV", zap.Error(err))
		return
	}

	var cmap colMap
	var hasHeader bool
	if detectHeader(first) {
		hasHeader = true
		cmap, err = buildColMap(first)
		if err != nil {
			log.Fatal("ошибка заголовка CSV", zap.Error(err))
			return
		}
	} else {
		// assume name,lat,lon
		cmap = colMap{name: 0, lat: 1, lon: 2, city: -1}
	}

	cfg := config.Load(log)
	db := storage.ConnectDB(&cfg.DB, log)
	defer storage.CloseDB(db, log)

	// Prepare counters
	total := 0
	insertedOrUpdated := 0
	skipped := 0
	var errs []string

	// helper to process one record
	process := func(rec []string, line int) {
		total++
		// ensure length
		if len(rec) <= cmap.name || len(rec) <= cmap.lat || len(rec) <= cmap.lon {
			skipped++
			errs = append(errs, fmt.Sprintf("строка %d: мало колонок: %v", line, rec))
			return
		}
		name := strings.TrimSpace(rec[cmap.name])
		if name == "" {
			skipped++
			errs = append(errs, fmt.Sprintf("строка %d: пустое имя станции", line))
			return
		}
		lat, err1 := parseFloat(rec[cmap.lat])
		lon, err2 := parseFloat(rec[cmap.lon])
		if err1 != nil || err2 != nil {
			skipped++
			errs = append(errs, fmt.Sprintf("строка %d: неверные координаты lat=%q lon=%q", line, rec[cmap.lat], rec[cmap.lon]))
			return
		}
		cityVal := *city
		if cmap.city >= 0 && cmap.city < len(rec) {
			c := strings.TrimSpace(rec[cmap.city])
			if c != "" {
				cityVal = c
			}
		}
		if cityVal == "" {
			skipped++
			errs = append(errs, fmt.Sprintf("строка %d: не указан город (ни в --city, ни в колонке)", line))
			return
		}

		if *dry {
			insertedOrUpdated++
			return
		}
		// lon,lat order in ST_MakePoint
		res := db.Exec(`
            INSERT INTO ref_metro(city, name, geom)
            VALUES ($1, $2, ST_SetSRID(ST_MakePoint($4, $3), 4326)::geography)
            ON CONFLICT (city, name) DO UPDATE
            SET geom = EXCLUDED.geom`, cityVal, name, lat, lon)
		if res.Error != nil {
			skipped++
			errs = append(errs, fmt.Sprintf("строка %d: ошибка вставки: %v", line, res.Error))
			return
		}
		insertedOrUpdated += int(res.RowsAffected)
	}

	line := 1
	if hasHeader {
		// already consumed header at line 1
	} else {
		process(first, line)
	}

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("ошибка чтения CSV: %v", err))
			break
		}
		line++
		process(rec, line)
	}

	log.Info("import_metro summary",
		zap.Int("total_rows", total),
		zap.Int("affected", insertedOrUpdated),
		zap.Int("skipped", skipped),
		zap.Int("errors", len(errs)),
	)
	// print first few errors if any
	const maxErrs = 10
	for i := 0; i < len(errs) && i < maxErrs; i++ {
		log.Warn("import_metro error", zap.String("msg", errs[i]))
	}
}
