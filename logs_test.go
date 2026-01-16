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
	return &Logger{
		logger: slog.New(h),
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})},
		isOk: true,
		opts: Options{
			URL:               "http://localhost",
			APIKey:            "key",
			Source:            "src",
			DispatchEndpoint:  "/dispatch",
			HealthEndpoint:    "/health",
			HeartbeatInterval: time.Second,
			MinDispatchLevel:  slog.LevelDebug,
		},
	}
}

func TestNewLogger(t *testing.T) {
	h := slog.NewTextHandler(io.Discard, nil)
	l := New(h, Options{URL: "http://localhost", DispatchEndpoint: "/d", HealthEndpoint: "/h"})
	if l == nil {
		t.Fatal("New returned nil")
	}
}

func TestLogger_checkDispatcher(t *testing.T) {
	l := newTestLoggerLogs()
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

func TestLogger_Fatal_Error_Warn_Info_Debug(t *testing.T) {
	l := newTestLoggerLogs()
	l.isOk = true
	l.opts.MinDispatchLevel = slog.LevelDebug
	l.Fatal("fatal msg")
	l.Error("error msg")
	l.Warn("warn msg")
	l.Info("info msg")
	l.Debug("debug msg")
}

func TestLogger_FatalContext_ErrorContext_WarnContext_InfoContext_DebugContext(t *testing.T) {
	l := newTestLoggerLogs()
	ctx := context.Background()
	l.isOk = true
	l.opts.MinDispatchLevel = slog.LevelDebug
	l.FatalContext(ctx, "fatal ctx")
	l.ErrorContext(ctx, "error ctx")
	l.WarnContext(ctx, "warn ctx")
	l.InfoContext(ctx, "info ctx")
	l.DebugContext(ctx, "debug ctx")
}

func TestLogger_With_WithGroup(t *testing.T) {
	l := newTestLoggerLogs()
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
	ctx := context.Background()
	_ = l.Enabled(ctx, slog.LevelInfo)
	_ = l.Handler()
}

func TestLogger_Log_LogAttrs(t *testing.T) {
	l := newTestLoggerLogs()
	ctx := context.Background()
	l.isOk = true
	l.Log(ctx, slog.LevelInfo, "msg", "foo", 1)
	l.LogAttrs(ctx, slog.LevelInfo, "msg", slog.String("foo", "bar"))
}
