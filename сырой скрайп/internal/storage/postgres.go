package storage

import (
	"fmt"
	"skripe/config"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func ConnectDB(cfg *config.DBConfig, log *zap.Logger) *gorm.DB {
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{PrepareStmt: false})
	if err != nil {
		log.Fatal("Не удалось подключиться к базе данных", zap.Error(err))
		return nil
	}

	log.Info("Подключение к базе данных успешно установлено")
	return db
}

func CloseDB(db *gorm.DB, log *zap.Logger) {
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Error("Не удалось получить объект sql.DB для закрытия", zap.Error(err))
		return
	}
	if err := sqlDB.Close(); err != nil {
		log.Error("Ошибка при закрытии соединения с БД", zap.Error(err))
	} else {
		log.Info("Соединение с БД закрыто")
	}
}
