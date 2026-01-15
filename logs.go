package logs

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

type Logger struct {
	logger *slog.Logger
	client *http.Client
	url    string
	apiKey string
	source string
	isOk   bool

	MinDispatchLevel slog.Level
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

	healthEndpoint    = "/health"
	dispatchEndpoint  = "/api/log"
	heartbeatInterval = 5 * time.Minute
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

func New(handler slog.Handler, url, apiKey, source string) *Logger {
	url = strings.TrimSpace(url)

	l := &Logger{
		logger:           slog.New(handler),
		client:           &http.Client{Timeout: 5 * time.Second},
		url:              url,
		apiKey:           apiKey,
		source:           source,
		isOk:             false,
		MinDispatchLevel: slog.LevelWarn,
	}

	if !l.checkDispatcher() {
		l.logger.Error("Log dispatcher is not reachable, using local logger only")
		l.url = ""
	} else {
		l.logger.Info("Log dispatcher is reachable, using remote logging")

		go func() {
			ticker := time.NewTicker(heartbeatInterval)
			for range ticker.C {
				l.checkDispatcher()
			}
		}()
	}

	return l
}

func (l *Logger) checkDispatcher() bool {
	if l.url == "" {
		return false
	}

	res, err := l.client.Get(l.url + healthEndpoint)
	l.isOk = (err == nil && res.StatusCode == http.StatusOK)
	return l.isOk
}

func (l *Logger) Fatal(msg string, args ...any) {
	l.logger.Error(msg, args...)

	if l.isOk && l.MinDispatchLevel <= slog.LevelError {
		l.sendLog(ERROR, msg, args...)
	}

	os.Exit(1)
}

func (l *Logger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)

	if l.isOk && l.MinDispatchLevel <= slog.LevelError {
		l.sendLog(ERROR, msg, args...)
	}
}

func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.logger.ErrorContext(ctx, msg, args...)

	if l.isOk && l.MinDispatchLevel <= slog.LevelError {
		l.sendLog(ERROR, msg, args...)
	}
}

func (l *Logger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)

	if l.isOk {
		l.sendLog(WARN, msg, args...)
	}
}

func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.logger.WarnContext(ctx, msg, args...)

	if l.isOk && l.MinDispatchLevel <= slog.LevelWarn {
		l.sendLog(WARN, msg, args...)
	}
}

func (l *Logger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)

	if l.isOk {
		l.sendLog(INFO, msg, args...)
	}
}

func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.logger.InfoContext(ctx, msg, args...)

	if l.isOk && l.MinDispatchLevel <= slog.LevelInfo {
		l.sendLog(INFO, msg, args...)
	}
}

func (l *Logger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)

	if l.isOk {
		l.sendLog(DEBUG, msg, args...)
	}
}

func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.logger.DebugContext(ctx, msg, args...)

	if l.isOk && l.MinDispatchLevel <= slog.LevelDebug {
		l.sendLog(DEBUG, msg, args...)
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
		logger: l.logger.With(attrs...),
		client: l.client,
		url:    l.url,
		apiKey: l.apiKey,
		isOk:   l.isOk,
	}
}

func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{
		logger: l.logger.WithGroup(name),
		client: l.client,
		url:    l.url,
		apiKey: l.apiKey,
		isOk:   l.isOk,
	}
}

func (l *Logger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	l.logger.Log(ctx, level, msg, args...)

	if l.isOk {
		levelStr, ok := logLevelsInverse[level]
		if !ok {
			levelStr = INFO
		}
		l.sendLog(levelStr, msg, args...)
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

		l.sendLog(levelStr, msg, args...)
	}
}
