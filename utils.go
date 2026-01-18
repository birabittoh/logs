package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func isStatusOK(code int) bool {
	return code >= 200 && code < 300
}

func (l *Logger) checkDispatcher() bool {
	if l.opts.URL == "" {
		return false
	}

	url := l.opts.URL + l.opts.HealthEndpoint
	res, err := l.client.Get(url)
	l.isOk = (err == nil && isStatusOK(res.StatusCode))
	return l.isOk
}

func (l *Logger) enqueueLog(level, msg string, args ...any) {
	data := make(map[string]string)
	length := len(args)
	if length%2 != 0 {
		args = append(args, "")
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

	l.queueMutex.Lock()
	defer l.queueMutex.Unlock()

	l.queue = append(l.queue, log)

	if len(l.queue) >= l.opts.MaxBatchSize {
		go l.flushQueue()
	}
}

func (l *Logger) flushQueue() {
	if l.opts.URL == "" {
		return
	}

	l.queueMutex.Lock()
	if len(l.queue) == 0 {
		l.queueMutex.Unlock()
		return
	}

	batch := make([]Log, len(l.queue))
	copy(batch, l.queue)
	l.queue = l.queue[:0]
	l.queueMutex.Unlock()

	req := l.prepareBatch(batch)
	if req == nil {
		return
	}

	l.dispatchBatch(req)
}

func (l *Logger) prepareBatch(batch []Log) *http.Request {
	logBytes, err := json.Marshal(batch)
	if err != nil {
		l.logger.Error("Failed to marshal batch to JSON", "error", err.Error())
		return nil
	}

	dispatchURL := l.opts.URL + l.opts.DispatchEndpoint
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, dispatchURL, bytes.NewReader(logBytes))
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

func (l *Logger) dispatchBatch(req *http.Request) {
	res, err := l.client.Do(req)
	if err != nil {
		l.isOk = false
		l.logger.Warn("Failed to send batch to dispatcher", "error", err.Error())
		return
	}

	defer res.Body.Close()
	if !isStatusOK(res.StatusCode) {
		l.logger.Warn("Dispatcher returned unexpected status", "code", res.StatusCode)
	}
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
