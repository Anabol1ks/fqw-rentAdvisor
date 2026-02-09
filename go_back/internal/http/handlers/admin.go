package handlers

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"go_back/internal/admin"
	"go_back/internal/config"
)

// AdminHandler — обработчик админ-панели
type AdminHandler struct {
	runner *admin.TaskRunner
	db     *gorm.DB
	cfg    *config.Config
	log    *zap.Logger
	mlPID  int // PID последнего ML-процесса (для перезапуска)
}

// NewAdminHandler создаёт новый AdminHandler
func NewAdminHandler(runner *admin.TaskRunner, db *gorm.DB, cfg *config.Config, log *zap.Logger) *AdminHandler {
	return &AdminHandler{
		runner: runner,
		db:     db,
		cfg:    cfg,
		log:    log,
	}
}

// ==================== AUTH MIDDLEWARE ====================

// AdminAuthMiddleware — проверка ключа администратора
func AdminAuthMiddleware(adminKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if adminKey == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "admin not configured"})
			c.Abort()
			return
		}

		key := c.GetHeader("X-Admin-Key")
		if key == "" {
			key = c.Query("key")
		}

		if key != adminKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid admin key"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ==================== DATA STATS ====================

type DataStats struct {
	RentCount    int64 `json:"rent_count"`
	SaleCount    int64 `json:"sale_count"`
	RentRawCount int64 `json:"rent_raw_count"`
	SaleRawCount int64 `json:"sale_raw_count"`
	TotalRaw     int64 `json:"total_raw"`
	Total        int64 `json:"total"`
}

// GetDataStats — количество записей в БД
func (h *AdminHandler) GetDataStats(c *gin.Context) {
	var stats DataStats

	// Counts from listing table
	h.db.Raw("SELECT COUNT(*) FROM listing WHERE deal_type = 'rent_long' AND is_active = true").Scan(&stats.RentCount)
	h.db.Raw("SELECT COUNT(*) FROM listing WHERE deal_type = 'sale' AND is_active = true").Scan(&stats.SaleCount)
	h.db.Raw("SELECT COUNT(*) FROM listing WHERE is_active = true").Scan(&stats.Total)

	// Counts from listing_raw
	h.db.Raw("SELECT COUNT(*) FROM listing_raw WHERE deal_type = 'rent_long'").Scan(&stats.RentRawCount)
	h.db.Raw("SELECT COUNT(*) FROM listing_raw WHERE deal_type = 'sale'").Scan(&stats.SaleRawCount)
	h.db.Raw("SELECT COUNT(*) FROM listing_raw").Scan(&stats.TotalRaw)

	c.JSON(http.StatusOK, stats)
}

// ==================== SCRAPING ====================

type ScrapeRequest struct {
	DealType        string `json:"deal_type" binding:"required"` // rent / sale
	City            string `json:"city"`
	Pages           int    `json:"pages"`
	MaxItems        int    `json:"max_items"`
	Parallelism     int    `json:"parallelism"`
	DelayMin        string `json:"delay_min"`
	DelayMax        string `json:"delay_max"`
	MaxEmptyPages   int    `json:"max_empty_pages"`
	MaxRetries      int    `json:"max_retries"`
	CaptchaCooldown string `json:"captcha_cooldown"`
	UseCookie       bool   `json:"use_cookie"`
}

// StartScrape — запуск скрапинга
func (h *AdminHandler) StartScrape(c *gin.Context) {
	var req ScrapeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Дефолты
	if req.City == "" {
		req.City = "moskva"
	}
	if req.Pages <= 0 {
		req.Pages = 100
	}
	if req.MaxItems <= 0 {
		req.MaxItems = 1000
	}
	if req.Parallelism <= 0 {
		req.Parallelism = 2
	}
	if req.DelayMin == "" {
		req.DelayMin = "1200ms"
	}
	if req.DelayMax == "" {
		req.DelayMax = "2000ms"
	}
	if req.MaxEmptyPages <= 0 {
		req.MaxEmptyPages = 2
	}
	if req.MaxRetries <= 0 {
		req.MaxRetries = 3
	}
	if req.CaptchaCooldown == "" {
		req.CaptchaCooldown = "2m"
	}

	deal := "rent"
	if req.DealType == "sale" {
		deal = "sale"
	}

	args := []string{
		"run", "./cmd/scrape_yandex",
		"--city=" + req.City,
		"--start=1",
		fmt.Sprintf("--pages=%d", req.Pages),
		fmt.Sprintf("--max-empty-pages=%d", req.MaxEmptyPages),
		fmt.Sprintf("--parallel=%d", req.Parallelism),
		"--delay-min=" + req.DelayMin,
		"--delay-max=" + req.DelayMax,
		"--deal=" + deal,
		fmt.Sprintf("--max-items=%d", req.MaxItems),
		"--use-referer=true",
		"--captcha-cooldown=" + req.CaptchaCooldown,
		fmt.Sprintf("--max-retries=%d", req.MaxRetries),
	}

	// Если нужно использовать куки из .env.resilient
	var env []string
	if req.UseCookie {
		cookieStr := h.loadCookieFromEnv()
		if cookieStr != "" {
			// Передаём куки через переменную окружения (безопаснее, чем через флаг)
			for _, e := range os.Environ() {
				env = append(env, e)
			}
			env = append(env, "COOKIE="+cookieStr)
		}
	}

	taskDef := admin.TaskDef{
		Name:    fmt.Sprintf("Скрапинг (%s)", deal),
		Cmd:     "go",
		Args:    args,
		WorkDir: h.cfg.ScrapDir,
		Env:     env,
	}

	task, err := h.runner.Start(taskDef)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id": task.ID,
		"name":    task.Name,
		"status":  task.Status,
	})
}

// ==================== SCRAP PIPELINE COMMANDS ====================

func (h *AdminHandler) RunScrapCommand(c *gin.Context) {
	command := c.Param("command")

	cmdMap := map[string]struct {
		name string
		args []string
	}{
		"normalize":    {"Нормализация", []string{"run", "./cmd/normalize", "--limit=0", "--batch=500", "--sleep-ms=0"}},
		"migrate":      {"Миграция БД", []string{"run", "./cmd/migrate"}},
		"geocode":      {"Геокодирование", []string{"run", "./cmd/geocode", "--workers=2", "--sleep=800ms", "--city=Москва", "--limit=0"}},
		"dedupe":       {"Дедупликация", []string{"run", "./cmd/dedupe"}},
		"enrich":       {"Обогащение", []string{"run", "./cmd/enrich", "--city=Москва"}},
		"deactivate":   {"Деактивация", []string{"run", "./cmd/deactivate"}},
		"fix-seq":      {"Фикс последовательностей", []string{"run", "./cmd/fix_sequences"}},
		"import-metro": {"Импорт метро", []string{"run", "./cmd/import_metro", "--file=./metro.ru.csv", "--city=Москва", "--sep=,"}},
		"pipeline":     {"Полный пайплайн обработки", nil}, // специальная обработка
	}

	def, ok := cmdMap[command]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown command: " + command})
		return
	}

	// Для pipeline запускаем последовательность через shell
	if command == "pipeline" {
		shellCmd := "go run ./cmd/normalize --limit=0 --batch=500 --sleep-ms=0 && go run ./cmd/enrich --city=Москва && go run ./cmd/dedupe"
		taskDef := admin.TaskDef{
			Name:    def.name,
			Cmd:     "cmd",
			Args:    []string{"/C", shellCmd},
			WorkDir: h.cfg.ScrapDir,
		}
		task, err := h.runner.Start(taskDef)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"task_id": task.ID, "name": task.Name, "status": task.Status})
		return
	}

	taskDef := admin.TaskDef{
		Name:    def.name,
		Cmd:     "go",
		Args:    def.args,
		WorkDir: h.cfg.ScrapDir,
	}

	task, err := h.runner.Start(taskDef)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id": task.ID,
		"name":    task.Name,
		"status":  task.Status,
	})
}

// ==================== ML PIPELINE ====================

func (h *AdminHandler) RunMLCommand(c *gin.Context) {
	command := c.Param("command")

	// Python виртуальное окружение
	pythonCmd := filepath.Join(h.cfg.RealvalDir, ".venv", "Scripts", "python.exe")
	if _, err := os.Stat(pythonCmd); os.IsNotExist(err) {
		pythonCmd = "python" // fallback
	}

	cmdMap := map[string]struct {
		name string
		args []string
	}{
		"export": {
			"Экспорт данных из БД",
			[]string{"-m", "realval", "db", "export",
				"--sql-file", "sql/export_listings_rent.sql",
				"--out", "data/raw/listings_rent.parquet"},
		},
		"ingest": {
			"Загрузка и очистка",
			[]string{"-m", "realval", "ingest",
				"--src", "data/raw/listings_rent.parquet",
				"--out", "data/interim/clean.parquet"},
		},
		"features": {
			"Генерация признаков",
			[]string{"-m", "realval", "features",
				"--in", "data/interim/clean.parquet",
				"--out", "data/features/base.parquet",
				"--h3-res", "7"},
		},
		"split": {
			"Разделение на train/valid",
			[]string{"-m", "realval", "split",
				"--in", "data/features/base.parquet",
				"--train-out", "data/splits/train.parquet",
				"--valid-out", "data/splits/valid.parquet",
				"--time-col", "first_seen",
				"--valid-days", "15",
				"--valid-ratio", "0.3"},
		},
		"train": {
			"Обучение модели",
			[]string{"-m", "realval", "train",
				"--train-path", "data/splits/train.parquet",
				"--valid-path", "data/splits/valid.parquet",
				"--model-out", "models/artefacts/rent_lgbm.joblib",
				"--target", "price_rub",
				"--log-target",
				"--with-intervals",
				"--use-local-stats"},
		},
		"predict": {
			"Тестовый прогноз",
			[]string{"-m", "realval", "predict-one",
				"--model-path", "models/artefacts/rent_lgbm.joblib",
				"--input-json", "sample_object.json",
				"--out", "models/reports/predict_sample.json"},
		},
	}

	def, ok := cmdMap[command]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown ML command: " + command})
		return
	}

	taskDef := admin.TaskDef{
		Name:    def.name,
		Cmd:     pythonCmd,
		Args:    def.args,
		WorkDir: h.cfg.RealvalDir,
		Env:     []string{"PYTHONPATH=" + filepath.Join(h.cfg.RealvalDir, "src")},
	}

	task, err := h.runner.Start(taskDef)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id": task.ID,
		"name":    task.Name,
		"status":  task.Status,
	})
}

// ==================== ML SERVER RESTART ====================

func (h *AdminHandler) RestartMLServer(c *gin.Context) {
	pythonCmd := filepath.Join(h.cfg.RealvalDir, ".venv", "Scripts", "python.exe")
	if _, err := os.Stat(pythonCmd); os.IsNotExist(err) {
		pythonCmd = "python"
	}

	// Останавливаем предыдущий ML-сервер (если запущен как задача)
	tasks := h.runner.List(50)
	for _, t := range tasks {
		if strings.Contains(t.Name, "ML-сервер") && t.Status == admin.TaskStatusRunning {
			_ = h.runner.Stop(t.ID)
			time.Sleep(2 * time.Second)
		}
	}

	taskDef := admin.TaskDef{
		Name:    "ML-сервер (uvicorn)",
		Cmd:     pythonCmd,
		Args:    []string{"-m", "uvicorn", "realval.http_app:app", "--reload", "--port", "8000", "--host", "0.0.0.0"},
		WorkDir: h.cfg.RealvalDir,
		Env:     []string{"PYTHONPATH=" + filepath.Join(h.cfg.RealvalDir, "src")},
	}

	task, err := h.runner.Start(taskDef)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id": task.ID,
		"name":    task.Name,
		"status":  task.Status,
	})
}

// ==================== TASKS MANAGEMENT ====================

type TaskInfo struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Command   string           `json:"command"`
	Status    admin.TaskStatus `json:"status"`
	StartedAt time.Time        `json:"started_at"`
	EndedAt   *time.Time       `json:"ended_at,omitempty"`
	ExitCode  *int             `json:"exit_code,omitempty"`
	Error     string           `json:"error,omitempty"`
}

func taskToInfo(t *admin.Task) TaskInfo {
	return TaskInfo{
		ID:        t.ID,
		Name:      t.Name,
		Command:   t.Command,
		Status:    t.Status,
		StartedAt: t.StartedAt,
		EndedAt:   t.EndedAt,
		ExitCode:  t.ExitCode,
		Error:     t.Error,
	}
}

// ListTasks — список всех задач
func (h *AdminHandler) ListTasks(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "30")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 30
	}

	tasks := h.runner.List(limit)
	result := make([]TaskInfo, len(tasks))
	for i, t := range tasks {
		result[i] = taskToInfo(t)
	}

	c.JSON(http.StatusOK, gin.H{"tasks": result})
}

// GetTask — конкретная задача с логами
func (h *AdminHandler) GetTask(c *gin.Context) {
	id := c.Param("id")
	task, ok := h.runner.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task": taskToInfo(task),
		"logs": task.GetLogs(),
	})
}

// StopTask — остановить задачу
func (h *AdminHandler) StopTask(c *gin.Context) {
	id := c.Param("id")
	if err := h.runner.Stop(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// StreamTaskLogs — SSE-поток логов задачи
func (h *AdminHandler) StreamTaskLogs(c *gin.Context) {
	id := c.Param("id")
	task, ok := h.runner.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Отправляем существующие логи
	for _, line := range task.GetLogs() {
		fmt.Fprintf(c.Writer, "data: %s\n\n", line)
	}
	c.Writer.Flush()

	// Если задача уже завершена — завершаем
	if task.Status != admin.TaskStatusRunning {
		fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", task.Status)
		c.Writer.Flush()
		return
	}

	// Подписываемся на поток
	ch := task.Subscribe()
	defer task.Unsubscribe(ch)

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-ch:
			if !ok {
				fmt.Fprintf(c.Writer, "event: done\ndata: closed\n\n")
				c.Writer.Flush()
				return
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", line)
			c.Writer.Flush()

			// Проверяем завершение
			if task.Status != admin.TaskStatusRunning {
				fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", task.Status)
				c.Writer.Flush()
				return
			}
		}
	}
}

// ==================== COOKIE MANAGEMENT ====================

type CookieData struct {
	Cookie string `json:"cookie"`
}

// GetCookies — получить текущие куки из .env.resilient
func (h *AdminHandler) GetCookies(c *gin.Context) {
	envFile := filepath.Join(h.cfg.ScrapDir, ".env.resilient")
	cookie := h.readCookieFromFile(envFile)
	c.JSON(http.StatusOK, CookieData{Cookie: cookie})
}

// UpdateCookies — обновить куки в .env.resilient
func (h *AdminHandler) UpdateCookies(c *gin.Context) {
	var req CookieData
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	envFile := filepath.Join(h.cfg.ScrapDir, ".env.resilient")

	if err := h.updateCookieInFile(envFile, req.Cookie); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// ==================== HELPERS ====================

func (h *AdminHandler) loadCookieFromEnv() string {
	envFile := filepath.Join(h.cfg.ScrapDir, ".env.resilient")
	return h.readCookieFromFile(envFile)
}

func (h *AdminHandler) readCookieFromFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return ""
	}

	text := string(content)

	// Ищем COOKIE="..."
	re := regexp.MustCompile(`(?s)COOKIE="(.*?)"`)
	matches := re.FindStringSubmatch(text)
	if len(matches) >= 2 {
		return matches[1]
	}

	return ""
}

func (h *AdminHandler) updateCookieInFile(path, newCookie string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	text := string(content)

	// Заменяем секцию COOKIE="..."
	re := regexp.MustCompile(`(?s)COOKIE=".*?"`)
	if re.MatchString(text) {
		text = re.ReplaceAllString(text, fmt.Sprintf(`COOKIE="%s"`, newCookie))
	} else {
		text += fmt.Sprintf("\nCOOKIE=\"%s\"\n", newCookie)
	}

	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// ==================== SCRAPE ENV CONFIG ====================

type ScrapeConfig struct {
	City            string `json:"city"`
	Pages           int    `json:"pages"`
	MaxItems        int    `json:"max_items"`
	Parallelism     int    `json:"parallelism"`
	DelayMin        string `json:"delay_min"`
	DelayMax        string `json:"delay_max"`
	MaxRetries      int    `json:"max_retries"`
	CaptchaCooldown string `json:"captcha_cooldown"`
	MaxEmptyPages   int    `json:"max_empty_pages"`
	DealType        string `json:"deal_type"`
}

// GetScrapeConfig — получить текущие настройки скрапинга
func (h *AdminHandler) GetScrapeConfig(c *gin.Context) {
	envFile := filepath.Join(h.cfg.ScrapDir, ".env.resilient")
	cfg := h.readScrapeConfig(envFile)
	c.JSON(http.StatusOK, cfg)
}

func (h *AdminHandler) readScrapeConfig(path string) ScrapeConfig {
	cfg := ScrapeConfig{
		City:            "moskva",
		Pages:           100,
		MaxItems:        1000,
		Parallelism:     2,
		DelayMin:        "1200ms",
		DelayMax:        "2000ms",
		MaxRetries:      3,
		CaptchaCooldown: "2m",
		MaxEmptyPages:   2,
		DealType:        "rent",
	}

	f, err := os.Open(path)
	if err != nil {
		return cfg
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "CITY":
			cfg.City = val
		case "PAGES":
			if v, err := strconv.Atoi(val); err == nil {
				cfg.Pages = v
			}
		case "MAX_ITEMS":
			if v, err := strconv.Atoi(val); err == nil {
				cfg.MaxItems = v
			}
		case "PARALLELISM":
			if v, err := strconv.Atoi(val); err == nil {
				cfg.Parallelism = v
			}
		case "DELAY_MIN":
			cfg.DelayMin = val
		case "DELAY_MAX":
			cfg.DelayMax = val
		case "MAX_RETRIES":
			if v, err := strconv.Atoi(val); err == nil {
				cfg.MaxRetries = v
			}
		case "CAPTCHA_COOLDOWN":
			cfg.CaptchaCooldown = val
		case "MAX_EMPTY_PAGES":
			if v, err := strconv.Atoi(val); err == nil {
				cfg.MaxEmptyPages = v
			}
		case "DEAL_TYPE":
			cfg.DealType = val
		}
	}

	return cfg
}
