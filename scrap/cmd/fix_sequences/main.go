package main

import (
	"context"
	"fmt"
	"os"
	"skripe/config"
	"skripe/internal/storage"

	"github.com/Anabol1ks/rentAdvisor-utils-go/logger"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

// Эта утилита чинит рассинхронизацию последовательностей PK после ручного импорта данных.
// Для таблиц с auto-increment id (listing_raw, listing, price_history) выставляет nextval = max(id)+1.
func main() {
	_ = godotenv.Load()
	if err := logger.Init(true); err != nil {
		panic(err)
	}
	defer logger.Sync()
	log := logger.L()

	cfg := config.Load(log)
	db := storage.ConnectDB(&cfg.DB, log)
	defer storage.CloseDB(db, log)

	ctx := context.Background()
	tables := []string{
		"public.listing_raw",
		"public.listing",
		"public.price_history",
	}

	for _, tbl := range tables {
		var seqName *string
		// Получим привязанную последовательность (работает для SERIAL/IDENTITY)
		if err := db.WithContext(ctx).Raw(
			"SELECT pg_get_serial_sequence(?, 'id')",
			tbl,
		).Scan(&seqName).Error; err != nil {
			log.Error("get serial sequence", zap.Error(err), zap.String("table", tbl))
			continue
		}
		if seqName == nil || *seqName == "" {
			log.Info("no serial/identity sequence for id (skip)", zap.String("table", tbl))
			continue
		}

		// Установим nextval = max(id)+1. Используем is_called=false, чтобы nextval вернул ровно заданное значение.
		q := fmt.Sprintf("SELECT setval('%s', COALESCE((SELECT MAX(id) FROM %s), 0) + 1, false)", *seqName, tbl)
		if err := db.WithContext(ctx).Exec(q).Error; err != nil {
			log.Error("set sequence value", zap.Error(err), zap.String("table", tbl), zap.String("sequence", *seqName))
			continue
		}
		log.Info("sequence reset", zap.String("table", tbl), zap.String("sequence", *seqName))
	}

	_ = db.WithContext(ctx).Exec("SELECT 1").Error
	_ = os.Stdout
}
