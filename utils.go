package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (l *Logger) checkDispatcher() bool {
	if l.opts.URL == "" {
		return false
	}

	url := l.opts.URL + l.opts.HealthEndpoint
	res, err := l.client.Get(url)
	l.isOk = (err == nil && res.StatusCode == http.StatusOK)
	return l.isOk
}

func (l *Logger) prepare(level, msg string, args ...any) *http.Request {
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
		Source:    l.opts.Source,
		Args:      data,
	}
	// DEBUG: Print Args to diagnose marshal error test
	fmt.Printf("prepare: Args = %#v\n", data)

	logBytes, err := json.Marshal(log)
	if err != nil {
		l.logger.Error("Failed to marshal log to JSON", "error", err.Error())
		return nil
	}

	dispatchURL := l.opts.URL + l.opts.DispatchEndpoint
	reader := strings.NewReader(string(logBytes))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, dispatchURL, reader)
	if err != nil {
		l.logger.Error("Failed to create request to dispatcher", "error", err.Error())
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	if l.opts.APIKey != "" {
		req.Header.Set("X-API-Key", l.opts.APIKey)
	}

	return req
}

func (l *Logger) dispatch(req *http.Request) {
	res, err := l.client.Do(req)
	if err != nil {
		l.isOk = false
		l.logger.Warn("Failed to send log to dispatcher", "error", err.Error())
		return
	}

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		l.logger.Warn("Dispatcher returned unexpected status", "code", res.StatusCode)
	}
}

func (l *Logger) sendLog(level, msg string, args ...any) {
	if l.opts.URL == "" {
		return
	}

	req := l.prepare(level, msg, args...)
	if req == nil {
		return
	}

	go l.dispatch(req)
}

func (l *Logger) sendLogSync(level, msg string, args ...any) {
	if l.opts.URL == "" {
		return
	}

	req := l.prepare(level, msg, args...)
	if req == nil {
		return
	}

	l.dispatch(req)
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
