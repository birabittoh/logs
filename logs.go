package logs

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type Logger struct {
	logger     *slog.Logger
	client     *http.Client
	isOk       bool
	opts       Options
	queue      []Log
	queueMutex sync.Mutex
	stopChan   chan struct{}
}

type Options struct {
	URL               string
	APIKey            string
	Source            string
	DispatchEndpoint  string
	HealthEndpoint    string
	HeartbeatInterval time.Duration
	MinDispatchLevel  slog.Level
	BatchInterval     time.Duration
	MaxBatchSize      int
}

type Log struct {
	Timestamp time.Time         `json:"timestamp"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Args      map[string]string `json:"args,omitempty"`
	Source    string            `json:"source,omitempty"`
}

const (
	DEBUG = "DEBUG"
	INFO  = "INFO"
	WARN  = "WARN"
	ERROR = "ERROR"
)

var (
	logLevels = map[string]slog.Level{
		DEBUG: slog.LevelDebug,
		INFO:  slog.LevelInfo,
		WARN:  slog.LevelWarn,
		ERROR: slog.LevelError,
	}

	logLevelsInverse = map[slog.Level]string{
		slog.LevelDebug: DEBUG,
		slog.LevelInfo:  INFO,
		slog.LevelWarn:  WARN,
		slog.LevelError: ERROR,
	}
)

func ParseLogLevel(levelStr string) slog.Level {
	level, ok := logLevels[strings.ToUpper(levelStr)]
	if !ok {
		level = slog.LevelInfo
	}
	return level
}

func New(handler slog.Handler, opts Options) *Logger {
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 5 * time.Minute
	}

	if opts.BatchInterval <= 0 {
		opts.BatchInterval = 5 * time.Second
	}

	if opts.MaxBatchSize <= 0 {
		opts.MaxBatchSize = 100
	}

	if opts.DispatchEndpoint == "" {
		opts.DispatchEndpoint = "/api/log"
	}

	if opts.HealthEndpoint == "" {
		opts.HealthEndpoint = opts.DispatchEndpoint
	}

	l := &Logger{
		logger:   slog.New(handler),
		client:   &http.Client{Timeout: 5 * time.Second},
		isOk:     false,
		opts:     opts,
		queue:    make([]Log, 0),
		stopChan: make(chan struct{}),
	}

	if !l.checkDispatcher() {
		l.logger.Error("Log dispatcher is not reachable, using local logger only")
		l.opts.URL = ""
	} else {
		l.logger.Info("Log dispatcher is reachable, using remote logging")

		go func() {
			ticker := time.NewTicker(l.opts.HeartbeatInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					l.checkDispatcher()
				case <-l.stopChan:
					return
				}
			}
		}()

		go func() {
			ticker := time.NewTicker(l.opts.BatchInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					l.flushQueue()
				case <-l.stopChan:
					l.flushQueue()
					return
				}
			}
		}()
	}

	return l
}

func (l *Logger) Close() {
	close(l.stopChan)
	time.Sleep(100 * time.Millisecond)
}

func (l *Logger) Fatal(msg string, args ...any) {
	l.logger.Error(msg, args...)

	if l.isOk && l.opts.MinDispatchLevel <= slog.LevelError {
		l.enqueueLog(ERROR, msg, args...)
		l.flushQueue()
	}

	os.Exit(1)
}

func (l *Logger) FatalContext(ctx context.Context, msg string, args ...any) {
	l.logger.ErrorContext(ctx, msg, args...)

	if l.isOk && l.opts.MinDispatchLevel <= slog.LevelError {
		l.enqueueLog(ERROR, msg, args...)
		l.flushQueue()
	}

	os.Exit(1)
}

func (l *Logger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)

	if l.isOk && l.opts.MinDispatchLevel <= slog.LevelError {
		l.enqueueLog(ERROR, msg, args...)
	}
}

func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.logger.ErrorContext(ctx, msg, args...)

	if l.isOk && l.opts.MinDispatchLevel <= slog.LevelError {
		l.enqueueLog(ERROR, msg, args...)
	}
}

func (l *Logger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)

	if l.isOk && l.opts.MinDispatchLevel <= slog.LevelWarn {
		l.enqueueLog(WARN, msg, args...)
	}
}

func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.logger.WarnContext(ctx, msg, args...)

	if l.isOk && l.opts.MinDispatchLevel <= slog.LevelWarn {
		l.enqueueLog(WARN, msg, args...)
	}
}

func (l *Logger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)

	if l.isOk && l.opts.MinDispatchLevel <= slog.LevelInfo {
		l.enqueueLog(INFO, msg, args...)
	}
}

func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.logger.InfoContext(ctx, msg, args...)

	if l.isOk && l.opts.MinDispatchLevel <= slog.LevelInfo {
		l.enqueueLog(INFO, msg, args...)
	}
}

func (l *Logger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)

	if l.isOk && l.opts.MinDispatchLevel <= slog.LevelDebug {
		l.enqueueLog(DEBUG, msg, args...)
	}
}

func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.logger.DebugContext(ctx, msg, args...)

	if l.isOk && l.opts.MinDispatchLevel <= slog.LevelDebug {
		l.enqueueLog(DEBUG, msg, args...)
	}
}

func (l *Logger) Enabled(ctx context.Context, level slog.Level) bool {
	return l.logger.Enabled(ctx, level)
}

func (l *Logger) Handler() slog.Handler {
	return l.logger.Handler()
}

func (l *Logger) With(attrs ...any) *Logger {
	return &Logger{
		logger:   l.logger.With(attrs...),
		client:   l.client,
		isOk:     l.isOk,
		opts:     l.opts,
		queue:    l.queue,
		stopChan: l.stopChan,
	}
}

func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{
		logger:   l.logger.WithGroup(name),
		client:   l.client,
		isOk:     l.isOk,
		opts:     l.opts,
		queue:    l.queue,
		stopChan: l.stopChan,
	}
}

func (l *Logger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	l.logger.Log(ctx, level, msg, args...)

	if l.isOk {
		levelStr, ok := logLevelsInverse[level]
		if !ok {
			levelStr = INFO
		}
		l.enqueueLog(levelStr, msg, args...)
	}
}

func (l *Logger) LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	l.logger.LogAttrs(ctx, level, msg, attrs...)

	if l.isOk {
		levelStr, ok := logLevelsInverse[level]
		if !ok {
			levelStr = INFO
		}

		args := make([]any, 0, len(attrs)*2)
		for _, attr := range attrs {
			args = append(args, attr.Key, attr.Value.Any())
		}

		l.enqueueLog(levelStr, msg, args...)
	}
}
