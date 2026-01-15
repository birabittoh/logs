package logs

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func ParseLogLevel(levelStr string) slog.Level {
	level, ok := logLevels[strings.ToUpper(levelStr)]
	if !ok {
		level = slog.LevelInfo
	}
	return level
}

func (l *Logger) sendLog(level, msg string, args ...any) {
	if l.url == "" {
		return
	}

	data := make(map[string]string)
	length := len(args)
	if length%2 != 0 {
		length-- // ignore last arg if odd
	}

	for i := 0; i < length; i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		data[key] = stringify(args[i+1])
	}

	log := Log{
		Level:     level,
		Message:   msg,
		Timestamp: time.Now(),
		Source:    l.source,
		Args:      data,
	}

	logBytes, err := json.Marshal(log)
	if err != nil {
		return
	}

	go func() {
		res, err := l.client.Post(l.url+dispatchEndpoint, "application/json", strings.NewReader(string(logBytes)))
		if err != nil {
			l.isOk = false
			l.logger.Warn("Failed to send log to dispatcher", "error", err.Error())
			return
		}

		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			l.logger.Warn("Dispatcher returned unexpected status", "code", res.StatusCode)
		}
	}()
}

func stringify(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%f", val)
	case bool:
		return fmt.Sprintf("%t", val)
	case fmt.Stringer:
		return val.String()
	case error:
		return val.Error()
	default:
		return fmt.Sprintf("%v", val)
	}
}
