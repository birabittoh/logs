package logs

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"DEBUG", slog.LevelDebug},
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo},
	}
	for _, c := range cases {
		if got := ParseLogLevel(c.in); got != c.want {
			t.Errorf("ParseLogLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

type stringer struct{}

func (s stringer) String() string { return "stringer" }

type testErr struct{}

func (e testErr) Error() string { return "err" }

func TestStringify(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"str", "str"},
		{42, "42"},
		{3.14, "3.140000"},
		{true, "true"},
		{stringer{}, "stringer"},
		{testErr{}, "err"},
		{[]int{1, 2}, "[1 2]"},
	}
	for _, c := range cases {
		if got := stringify(c.in); got != c.want {
			t.Errorf("stringify(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func newTestLogger() *Logger {
	h := slog.NewTextHandler(io.Discard, nil)
	return &Logger{
		logger: slog.New(h),
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})},
		isOk:     true,
		queue:    make([]Log, 0),
		stopChan: make(chan struct{}),
		opts: Options{
			URL:              "http://localhost",
			APIKey:           "key",
			Source:           "src",
			DispatchEndpoint: "/dispatch",
			BatchInterval:    100 * time.Millisecond,
			MaxBatchSize:     10,
		},
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLogger_enqueueLog(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	l.enqueueLog("INFO", "msg", "foo", 1, "bar", true)

	l.queueMutex.Lock()
	defer l.queueMutex.Unlock()

	if len(l.queue) != 1 {
		t.Fatalf("expected 1 log in queue, got %d", len(l.queue))
	}

	log := l.queue[0]
	if log.Level != "INFO" || log.Message != "msg" || log.Source != "src" {
		t.Errorf("unexpected log fields: %+v", log)
	}
	if log.Args["foo"] != "1" || log.Args["bar"] != "true" {
		t.Errorf("unexpected args: %+v", log.Args)
	}
}

func TestLogger_enqueueLog_invalidArgs(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	l.enqueueLog("INFO", "msg", 123, "foo", "bar")

	l.queueMutex.Lock()
	defer l.queueMutex.Unlock()

	if len(l.queue) != 1 {
		t.Fatalf("expected 1 log in queue for invalid args, got %d", len(l.queue))
	}
}

func TestLogger_enqueueLog_maxBatchSize(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	l.opts.MaxBatchSize = 2
	l.isOk = true

	l.enqueueLog("INFO", "msg1")
	l.enqueueLog("INFO", "msg2")

	time.Sleep(50 * time.Millisecond)

	l.queueMutex.Lock()
	queueLen := len(l.queue)
	l.queueMutex.Unlock()

	if queueLen != 0 {
		t.Errorf("expected queue to be flushed when max batch size reached, got %d logs", queueLen)
	}
}

func TestLogger_prepareBatch(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	batch := []Log{
		{Level: "INFO", Message: "msg1", Timestamp: time.Now(), Source: "src"},
		{Level: "ERROR", Message: "msg2", Timestamp: time.Now(), Source: "src"},
	}

	req := l.prepareBatch(batch)
	if req == nil {
		t.Fatal("prepareBatch returned nil")
	}

	if req.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", req.Method)
	}

	if req.Header.Get("X-API-Key") != "key" {
		t.Errorf("expected API key header")
	}

	var logs []Log
	err := json.NewDecoder(req.Body).Decode(&logs)
	if err != nil {
		t.Fatalf("failed to decode batch: %v", err)
	}

	if len(logs) != 2 {
		t.Errorf("expected 2 logs in batch, got %d", len(logs))
	}
}

func TestLogger_prepareBatch_requestError(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	l.opts.URL = string([]byte{0x7f}) // invalid URL

	batch := []Log{{Level: "INFO", Message: "msg", Timestamp: time.Now()}}
	req := l.prepareBatch(batch)
	if req != nil {
		t.Error("expected nil request on request error")
	}
}

func TestLogger_dispatchBatch_success(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	batch := []Log{{Level: "INFO", Message: "msg", Timestamp: time.Now(), Source: "src"}}
	req := l.prepareBatch(batch)
	l.dispatchBatch(req)

	if !l.isOk {
		t.Error("isOk should be true on success")
	}
}

func TestLogger_dispatchBatch_error(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	l.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("fail")
	})}

	batch := []Log{{Level: "INFO", Message: "msg", Timestamp: time.Now(), Source: "src"}}
	req := l.prepareBatch(batch)
	l.dispatchBatch(req)

	if l.isOk {
		t.Error("isOk should be false on error")
	}
}

func TestLogger_dispatchBatch_statusNotOK(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	l.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader("fail"))}, nil
	})}

	batch := []Log{{Level: "INFO", Message: "msg", Timestamp: time.Now(), Source: "src"}}
	req := l.prepareBatch(batch)
	l.dispatchBatch(req)
}

func TestLogger_flushQueue(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	l.enqueueLog("INFO", "msg1")
	l.enqueueLog("INFO", "msg2")

	l.flushQueue()

	l.queueMutex.Lock()
	queueLen := len(l.queue)
	l.queueMutex.Unlock()

	if queueLen != 0 {
		t.Errorf("expected queue to be empty after flush, got %d logs", queueLen)
	}
}

func TestLogger_flushQueue_emptyURL(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	l.opts.URL = ""
	l.enqueueLog("INFO", "msg")
	l.flushQueue()
}

func TestLogger_flushQueue_emptyQueue(t *testing.T) {
	l := newTestLogger()
	defer l.Close()

	l.flushQueue()
}
