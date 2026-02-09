package admin

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TaskStatus — статус задачи
type TaskStatus string

const (
	TaskStatusPending TaskStatus = "pending"
	TaskStatusRunning TaskStatus = "running"
	TaskStatusDone    TaskStatus = "done"
	TaskStatusFailed  TaskStatus = "failed"
	TaskStatusStopped TaskStatus = "stopped"
)

// Task — описание запущенной/завершённой задачи
type Task struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Command   string     `json:"command"`
	Status    TaskStatus `json:"status"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	ExitCode  *int       `json:"exit_code,omitempty"`
	Error     string     `json:"error,omitempty"`

	mu        sync.RWMutex
	logs      []string
	maxLogs   int
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	listeners map[chan string]struct{}
}

// GetLogs возвращает буфер логов
func (t *Task) GetLogs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]string, len(t.logs))
	copy(out, t.logs)
	return out
}

// Subscribe подписывается на поток логов (SSE)
func (t *Task) Subscribe() chan string {
	t.mu.Lock()
	defer t.mu.Unlock()
	ch := make(chan string, 64)
	if t.listeners == nil {
		t.listeners = make(map[chan string]struct{})
	}
	t.listeners[ch] = struct{}{}
	return ch
}

// Unsubscribe отписывается от потока логов
func (t *Task) Unsubscribe(ch chan string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.listeners, ch)
	close(ch)
}

func (t *Task) appendLog(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.logs) >= t.maxLogs {
		t.logs = t.logs[1:]
	}
	t.logs = append(t.logs, line)
	for ch := range t.listeners {
		select {
		case ch <- line:
		default: // не блокируемся
		}
	}
}

// TaskRunner — менеджер процессов
type TaskRunner struct {
	mu     sync.RWMutex
	tasks  map[string]*Task
	log    *zap.Logger
	nextID int
}

// NewTaskRunner создаёт новый TaskRunner
func NewTaskRunner(log *zap.Logger) *TaskRunner {
	return &TaskRunner{
		tasks: make(map[string]*Task),
		log:   log,
	}
}

// TaskDef — определение задачи для запуска
type TaskDef struct {
	Name    string
	Cmd     string
	Args    []string
	WorkDir string
	Env     []string
}

// Start запускает новую задачу
func (r *TaskRunner) Start(def TaskDef) (*Task, error) {
	r.mu.Lock()
	r.nextID++
	id := fmt.Sprintf("task_%d_%d", time.Now().Unix(), r.nextID)
	r.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	cmdStr := def.Cmd + " " + joinArgs(def.Args)

	task := &Task{
		ID:        id,
		Name:      def.Name,
		Command:   cmdStr,
		Status:    TaskStatusRunning,
		StartedAt: time.Now(),
		maxLogs:   2000,
		cancel:    cancel,
		listeners: make(map[chan string]struct{}),
	}

	r.mu.Lock()
	r.tasks[id] = task
	r.mu.Unlock()

	cmd := exec.CommandContext(ctx, def.Cmd, def.Args...)
	if def.WorkDir != "" {
		cmd.Dir = def.WorkDir
	}
	if len(def.Env) > 0 {
		cmd.Env = def.Env
	}
	task.cmd = cmd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err.Error()
		now := time.Now()
		task.EndedAt = &now
		return task, err
	}
	cmd.Stderr = cmd.Stdout // объединяем stderr в stdout

	if err := cmd.Start(); err != nil {
		task.Status = TaskStatusFailed
		task.Error = err.Error()
		now := time.Now()
		task.EndedAt = &now
		return task, err
	}

	r.log.Info("Task started",
		zap.String("id", id),
		zap.String("name", def.Name),
		zap.String("cmd", cmdStr),
	)

	task.appendLog(fmt.Sprintf("[system] Задача запущена: %s", cmdStr))

	// Горутина чтения вывода
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			task.appendLog(scanner.Text())
		}
	}()

	// Горутина ожидания завершения
	go func() {
		err := cmd.Wait()
		now := time.Now()
		task.mu.Lock()
		task.EndedAt = &now
		if ctx.Err() == context.Canceled {
			task.Status = TaskStatusStopped
			task.appendLog("[system] Задача остановлена пользователем")
		} else if err != nil {
			task.Status = TaskStatusFailed
			task.Error = err.Error()
			if exitErr, ok := err.(*exec.ExitError); ok {
				code := exitErr.ExitCode()
				task.ExitCode = &code
			}
			task.appendLog(fmt.Sprintf("[system] Задача завершилась с ошибкой: %s", err.Error()))
		} else {
			task.Status = TaskStatusDone
			code := 0
			task.ExitCode = &code
			task.appendLog("[system] Задача завершена успешно")
		}
		task.mu.Unlock()

		r.log.Info("Task finished",
			zap.String("id", task.ID),
			zap.String("status", string(task.Status)),
		)
	}()

	return task, nil
}

// Stop останавливает задачу по ID
func (r *TaskRunner) Stop(id string) error {
	r.mu.RLock()
	task, ok := r.tasks[id]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if task.Status != TaskStatusRunning {
		return fmt.Errorf("task is not running: %s", task.Status)
	}
	task.cancel()
	return nil
}

// Get возвращает задачу по ID
func (r *TaskRunner) Get(id string) (*Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[id]
	return t, ok
}

// List возвращает все задачи (последние N)
func (r *TaskRunner) List(limit int) []*Task {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]*Task, 0, len(r.tasks))
	for _, t := range r.tasks {
		all = append(all, t)
	}
	// сортируем по дате запуска (новые сначала)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].StartedAt.After(all[i].StartedAt) {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

func joinArgs(args []string) string {
	s := ""
	for i, a := range args {
		if i > 0 {
			s += " "
		}
		s += a
	}
	return s
}
