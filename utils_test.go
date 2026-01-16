package logs

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
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
		isOk: true,
		opts: Options{
			URL:              "http://localhost",
			APIKey:           "key",
			Source:           "src",
			DispatchEndpoint: "/dispatch",
		},
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLogger_prepare(t *testing.T) {
	l := newTestLogger()
	req := l.prepare("INFO", "msg", "foo", 1, "bar", true)
	if req == nil {
		t.Fatal("prepare returned nil")
	}
	if req.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if req.Header.Get("X-API-Key") != "key" {
		t.Errorf("expected API key header")
	}
	var log Log
	_ = json.NewDecoder(req.Body).Decode(&log)
	if log.Level != "INFO" || log.Message != "msg" || log.Source != "src" {
		t.Errorf("unexpected log fields: %+v", log)
	}
	if log.Args["foo"] != "1" || log.Args["bar"] != "true" {
		t.Errorf("unexpected args: %+v", log.Args)
	}
}

func TestLogger_prepare_invalidArgs(t *testing.T) {
	l := newTestLogger()
	req := l.prepare("INFO", "msg", 123, "foo", "bar")
	if req == nil {
		t.Fatal("prepare returned nil for invalid args")
	}
}

func TestLogger_prepare_requestError(t *testing.T) {
	l := newTestLogger()
	l.opts.URL = string([]byte{0x7f}) // invalid URL
	req := l.prepare("INFO", "msg")
	if req != nil {
		t.Error("expected nil request on request error")
	}
}

func TestLogger_dispatch_success(t *testing.T) {
	l := newTestLogger()
	req := l.prepare("INFO", "msg")
	l.dispatch(req)
	if !l.isOk {
		t.Error("isOk should be true on success")
	}
}

func TestLogger_dispatch_error(t *testing.T) {
	l := newTestLogger()
	l.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("fail")
	})}
	req := l.prepare("INFO", "msg")
	l.dispatch(req)
	if l.isOk {
		t.Error("isOk should be false on error")
	}
}

func TestLogger_dispatch_statusNotOK(t *testing.T) {
	l := newTestLogger()
	l.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader("fail"))}, nil
	})}
	req := l.prepare("INFO", "msg")
	l.dispatch(req)
}

func TestLogger_sendLog(t *testing.T) {
	l := newTestLogger()
	l.sendLog("INFO", "msg")
	l.opts.URL = ""
	l.sendLog("INFO", "msg") // should do nothing
}

func TestLogger_sendLogSync(t *testing.T) {
	l := newTestLogger()
	l.sendLogSync("INFO", "msg")
	l.opts.URL = ""
	l.sendLogSync("INFO", "msg") // should do nothing
}
