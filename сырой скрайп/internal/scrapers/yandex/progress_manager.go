package yandex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

// ProgressState представляет состояние прогресса скрапинга
type ProgressState struct {
	City             string         `json:"city"`
	DealType         string         `json:"deal_type"`
	CurrentPage      int            `json:"current_page"`
	PagesVisited     int            `json:"pages_visited"`
	ItemsCollected   int64          `json:"items_collected"`
	EmptyStreak      int            `json:"empty_streak"`
	LastSuccessTime  time.Time      `json:"last_success_time"`
	TotalRequests    int64          `json:"total_requests"`
	SuccessfulReqs   int64          `json:"successful_requests"`
	FailedReqs       int64          `json:"failed_requests"`
	CaptchaCount     int64          `json:"captcha_encounters"`
	RetryStates      map[string]int `json:"retry_states"`
	SessionStartTime time.Time      `json:"session_start_time"`
	Version          string         `json:"version"`
}

// ProgressManager управляет сохранением и восстановлением прогресса
type ProgressManager struct {
	filePath string
	log      *zap.Logger
}

func NewProgressManager(filePath string, log *zap.Logger) *ProgressManager {
	return &ProgressManager{
		filePath: filePath,
		log:      log,
	}
}

// SaveProgress сохраняет текущий прогресс в файл
func (pm *ProgressManager) SaveProgress(state ProgressState) error {
	state.Version = "1.0"

	// Создаем директорию если не существует
	if err := os.MkdirAll(filepath.Dir(pm.filePath), 0755); err != nil {
		return fmt.Errorf("failed to create progress directory: %w", err)
	}

	// Сохраняем во временный файл, затем переименовываем (атомарная операция)
	tempFile := pm.filePath + ".tmp"

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal progress state: %w", err)
	}

	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write progress file: %w", err)
	}

	if err := os.Rename(tempFile, pm.filePath); err != nil {
		return fmt.Errorf("failed to rename progress file: %w", err)
	}

	pm.log.Debug("progress saved", zap.String("file", pm.filePath))
	return nil
}

// LoadProgress загружает прогресс из файла
func (pm *ProgressManager) LoadProgress() (*ProgressState, error) {
	if _, err := os.Stat(pm.filePath); os.IsNotExist(err) {
		return nil, nil // файл не существует, это нормально
	}

	data, err := os.ReadFile(pm.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read progress file: %w", err)
	}

	var state ProgressState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal progress state: %w", err)
	}

	pm.log.Info("progress loaded",
		zap.String("file", pm.filePath),
		zap.Int("current_page", state.CurrentPage),
		zap.Int64("items_collected", state.ItemsCollected))

	return &state, nil
}

// CleanupProgress удаляет файл прогресса (обычно после успешного завершения)
func (pm *ProgressManager) CleanupProgress() error {
	if err := os.Remove(pm.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to cleanup progress file: %w", err)
	}

	pm.log.Debug("progress file cleaned up", zap.String("file", pm.filePath))
	return nil
}

// IsProgressValid проверяет, валиден ли прогресс для восстановления
func (pm *ProgressManager) IsProgressValid(state *ProgressState, maxAge time.Duration) bool {
	if state == nil {
		return false
	}

	// Проверяем возраст сессии
	age := time.Since(state.SessionStartTime)
	if age > maxAge {
		pm.log.Info("progress too old, starting fresh session",
			zap.Duration("age", age),
			zap.Duration("max_age", maxAge))
		return false
	}

	// Проверяем, не была ли последняя активность слишком давно
	lastActivity := time.Since(state.LastSuccessTime)
	if lastActivity > maxAge/2 {
		pm.log.Info("last activity too old, starting fresh session",
			zap.Duration("last_activity", lastActivity))
		return false
	}

	return true
}

// GetProgressFilePath возвращает путь к файлу прогресса по умолчанию
func GetProgressFilePath(city, dealType string) string {
	return filepath.Join(".", "progress", fmt.Sprintf("scraper_%s_%s.json", city, dealType))
}

// CreateProgressBackup создает резервную копию файла прогресса
func (pm *ProgressManager) CreateProgressBackup() error {
	if _, err := os.Stat(pm.filePath); os.IsNotExist(err) {
		return nil // нет файла для бэкапа
	}

	backupPath := pm.filePath + ".backup." + time.Now().Format("20060102_150405")

	data, err := os.ReadFile(pm.filePath)
	if err != nil {
		return fmt.Errorf("failed to read progress file for backup: %w", err)
	}

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return fmt.Errorf("failed to create progress backup: %w", err)
	}

	pm.log.Info("progress backup created", zap.String("backup_file", backupPath))
	return nil
}

// ResilientScraperWithProgress расширяет ResilientScraper возможностью сохранения прогресса
type ResilientScraperWithProgress struct {
	*ResilientScraper
	progressManager *ProgressManager
	progressState   *ProgressState
}

func NewResilientScraperWithProgress(opts ResilientOptions, log *zap.Logger) *ResilientScraperWithProgress {
	scraper := NewResilientScraper(opts, log)

	progressFile := opts.ProgressFile
	if progressFile == "" {
		progressFile = GetProgressFilePath(opts.City, opts.DealType)
	}

	progressManager := NewProgressManager(progressFile, log)

	return &ResilientScraperWithProgress{
		ResilientScraper: scraper,
		progressManager:  progressManager,
	}
}

// RunWithProgressSaving запускает скрапер с сохранением прогресса
func (rs *ResilientScraperWithProgress) RunWithProgressSaving() error {
	// Попытка загрузить существующий прогресс
	if rs.opts.EnableProgressSave {
		if err := rs.progressManager.CreateProgressBackup(); err != nil {
			rs.log.Warn("failed to create progress backup", zap.Error(err))
		}

		if progress, err := rs.progressManager.LoadProgress(); err != nil {
			rs.log.Warn("failed to load progress", zap.Error(err))
		} else if rs.progressManager.IsProgressValid(progress, 24*time.Hour) {
			rs.log.Info("resuming from saved progress",
				zap.Int("page", progress.CurrentPage),
				zap.Int64("items", progress.ItemsCollected))

			// Применяем сохраненный прогресс
			rs.opts.StartPage = progress.CurrentPage
			rs.progressState = progress
		}
	}

	// Запускаем основной цикл скрапинга
	err := rs.ResilientScraper.RunResilient()

	// Очищаем прогресс при успешном завершении
	if err == nil && rs.opts.EnableProgressSave {
		if cleanupErr := rs.progressManager.CleanupProgress(); cleanupErr != nil {
			rs.log.Warn("failed to cleanup progress", zap.Error(cleanupErr))
		}
	}

	return err
}

// saveCurrentProgress сохраняет текущее состояние (должно вызываться периодически)
func (rs *ResilientScraperWithProgress) saveCurrentProgress(currentPage, pagesVisited int, itemsCollected int64, emptyStreak int) {
	if !rs.opts.EnableProgressSave {
		return
	}

	state := ProgressState{
		City:             rs.opts.City,
		DealType:         rs.opts.DealType,
		CurrentPage:      currentPage,
		PagesVisited:     pagesVisited,
		ItemsCollected:   itemsCollected,
		EmptyStreak:      emptyStreak,
		LastSuccessTime:  time.Now(),
		TotalRequests:    rs.session.TotalRequests,
		SuccessfulReqs:   rs.session.SuccessfulRequests,
		FailedReqs:       rs.session.FailedRequests,
		CaptchaCount:     rs.session.CaptchaEncounters,
		RetryStates:      make(map[string]int),
		SessionStartTime: rs.session.StartTime,
	}

	// Копируем retry states
	rs.mu.RLock()
	for url, retryState := range rs.retryStates {
		state.RetryStates[url] = retryState.Attempts
	}
	rs.mu.RUnlock()

	if err := rs.progressManager.SaveProgress(state); err != nil {
		rs.log.Warn("failed to save progress", zap.Error(err))
	}
}
