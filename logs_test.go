package logs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func newTestLoggerLogs() *Logger {
	h := slog.NewTextHandler(io.Discard, nil)
	l := &Logger{
		logger: slog.New(h),
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})},
		isOk:     true,
		queue:    make([]Log, 0),
		stopChan: make(chan struct{}),
		opts: Options{
			URL:               "http://localhost",
			APIKey:            "key",
			Source:            "src",
			DispatchEndpoint:  "/dispatch",
			HealthEndpoint:    "/health",
			HeartbeatInterval: time.Second,
			MinDispatchLevel:  slog.LevelDebug,
			BatchInterval:     100 * time.Millisecond,
			MaxBatchSize:      10,
		},
	}

	// Start batch interval goroutine
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

	return l
}

func TestNewLogger(t *testing.T) {
	h := slog.NewTextHandler(io.Discard, nil)
	l := New(h, Options{URL: "http://localhost", DispatchEndpoint: "/d", HealthEndpoint: "/h"})
	if l == nil {
		t.Fatal("New returned nil")
	}
	defer l.Close()

	if l.opts.BatchInterval != 5*time.Second {
		t.Error("default BatchInterval should be 5 seconds")
	}
	if l.opts.MaxBatchSize != 100 {
		t.Error("default MaxBatchSize should be 100")
	}
}

func TestLogger_checkDispatcher(t *testing.T) {
	l := newTestLoggerLogs()
	defer l.Close()

	l.opts.URL = ""
	if l.checkDispatcher() {
		t.Error("should be false if URL is empty")
	}

	l.opts.URL = "http://localhost"
	l.opts.HealthEndpoint = "/health"
	l.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})}
	if !l.checkDispatcher() {
		t.Error("should be true if healthy")
	}

	l.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("fail")
	})}
	if l.checkDispatcher() {
		t.Error("should be false on error")
	}
}

func TestLogger_Error_Warn_Info_Debug(t *testing.T) {
	l := newTestLoggerLogs()
	defer l.Close()

	l.isOk = true
	l.opts.MinDispatchLevel = slog.LevelDebug

	l.Error("error msg")
	l.Warn("warn msg")
	l.Info("info msg")
	l.Debug("debug msg")

	time.Sleep(50 * time.Millisecond)
}

func TestLogger_ErrorContext_WarnContext_InfoContext_DebugContext(t *testing.T) {
	l := newTestLoggerLogs()
	defer l.Close()

	ctx := context.Background()
	l.isOk = true
	l.opts.MinDispatchLevel = slog.LevelDebug

	l.ErrorContext(ctx, "error ctx")
	l.WarnContext(ctx, "warn ctx")
	l.InfoContext(ctx, "info ctx")
	l.DebugContext(ctx, "debug ctx")

	time.Sleep(50 * time.Millisecond)
}

func TestLogger_With_WithGroup(t *testing.T) {
	l := newTestLoggerLogs()
	defer l.Close()

	l2 := l.With("foo", "bar")
	if l2 == nil {
		t.Error("With returned nil")
	}

	l3 := l.WithGroup("grp")
	if l3 == nil {
		t.Error("WithGroup returned nil")
	}
}

func TestLogger_Enabled_Handler(t *testing.T) {
	l := newTestLoggerLogs()
	defer l.Close()

	ctx := context.Background()
	_ = l.Enabled(ctx, slog.LevelInfo)
	_ = l.Handler()
}

func TestLogger_Log_LogAttrs(t *testing.T) {
	l := newTestLoggerLogs()
	defer l.Close()

	ctx := context.Background()
	l.isOk = true

	l.Log(ctx, slog.LevelInfo, "msg", "foo", 1)
	l.LogAttrs(ctx, slog.LevelInfo, "msg", slog.String("foo", "bar"))

	time.Sleep(50 * time.Millisecond)
}

func TestLogger_BatchFlush(t *testing.T) {
	l := newTestLoggerLogs()
	defer l.Close()

	l.isOk = true
	l.opts.MaxBatchSize = 3

	l.Info("msg1")
	l.Info("msg2")

	l.queueMutex.Lock()
	queueLen := len(l.queue)
	l.queueMutex.Unlock()

	if queueLen != 2 {
		t.Errorf("expected 2 logs in queue, got %d", queueLen)
	}

	l.Info("msg3")
	time.Sleep(50 * time.Millisecond)

	l.queueMutex.Lock()
	queueLen = len(l.queue)
	l.queueMutex.Unlock()

	if queueLen != 0 {
		t.Errorf("expected queue to be flushed, got %d logs", queueLen)
	}
}

func TestLogger_AutoFlush(t *testing.T) {
	l := newTestLoggerLogs()
	defer l.Close()

	l.isOk = true
	l.opts.BatchInterval = 50 * time.Millisecond

	l.Info("msg1")
	l.Info("msg2")

	// Wait for auto-flush
	time.Sleep(150 * time.Millisecond)

	l.queueMutex.Lock()
	queueLen := len(l.queue)
	l.queueMutex.Unlock()

	if queueLen != 0 {
		t.Errorf("expected queue to be auto-flushed, got %d logs", queueLen)
	}
}

func TestLogger_Close(t *testing.T) {
	l := newTestLoggerLogs()

	l.isOk = true
	l.Info("msg1")
	l.Info("msg2")

	l.queueMutex.Lock()
	queueLen := len(l.queue)
	l.queueMutex.Unlock()

	if queueLen != 2 {
		t.Errorf("expected 2 logs in queue before close, got %d", queueLen)
	}

	l.Close()

	l.queueMutex.Lock()
	queueLen = len(l.queue)
	l.queueMutex.Unlock()

	if queueLen != 0 {
		t.Errorf("expected queue to be flushed on close, got %d logs", queueLen)
	}
}
