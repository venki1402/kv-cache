package main

import (
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/valyala/fasthttp"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

func TestRunLifecycle(t *testing.T) {
	port, mport := freePort(t), freePort(t)
	t.Setenv("PORT", port)
	t.Setenv("METRICS_PORT", mport)
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("OTEL_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:"+freePort(t))
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "200")
	t.Setenv("SHUTDOWN_DELAY", "0s")
	t.Setenv("MEMORY_LIMIT_BYTES", "536870912")
	t.Setenv("MAX_MEMORY_BYTES", "4096") // 128 B per shard: one entry each, so puts evict
	defer func() { tracingEnabled = false }()

	done := make(chan error, 1)
	go func() { done <- run() }()

	base := "http://127.0.0.1:" + port
	client := &fasthttp.Client{}
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, _, err := client.Get(nil, base+"/readyz")
		if err == nil && code == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server not ready: code=%d err=%v", code, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Enough puts into a tiny cache to force traced evictions.
	for i := 0; i < 50; i++ {
		req := fasthttp.AcquireRequest()
		resp := fasthttp.AcquireResponse()
		req.Header.SetMethod("POST")
		req.SetRequestURI(base + "/put")
		req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
		req.SetBodyString(`{"key":"k` + strconv.Itoa(i) + `","value":"v"}`)
		if err := client.Do(req, resp); err != nil || resp.StatusCode() != 200 {
			t.Fatalf("put %d failed: %v %d", i, err, resp.StatusCode())
		}
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(resp)
	}
	if code, _, _ := client.Get(nil, base+"/get?key=k49"); code != 200 {
		t.Fatalf("expected 200 for recent key, got %d", code)
	}

	code, body, err := client.Get(nil, "http://127.0.0.1:"+mport+"/metrics")
	if err != nil || code != 200 || !strings.Contains(string(body), "kvcache_evictions_total") {
		t.Fatalf("metrics scrape failed: %v %d", err, code)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("server did not shut down")
	}
	if ready.Load() {
		t.Fatal("readiness should be false after shutdown")
	}
}

func TestRunInvalidConfig(t *testing.T) {
	t.Setenv("MAX_MEMORY_BYTES", "-1")
	if err := run(); err == nil {
		t.Fatal("expected config error")
	}
}

func TestLabelHelpers(t *testing.T) {
	for path, want := range map[string]string{
		"/get": "get", "/put": "put", "/health": "health", "/healthz": "healthz",
		"/readyz": "readyz", "/version": "version", "/x": "other",
	} {
		if got := routeNames[routeOf([]byte(path))]; got != want {
			t.Errorf("routeOf(%s) = %s, want %s", path, got, want)
		}
	}
	for m, want := range map[string]string{"GET": "GET", "POST": "POST", "PUT": "PUT", "DELETE": "OTHER"} {
		if got := methodLabel([]byte(m)); got != want {
			t.Errorf("methodLabel(%s) = %s, want %s", m, got, want)
		}
	}
	h := headerCarrier{&fasthttp.RequestHeader{}}
	h.Set("traceparent", "x")
	if h.Get("traceparent") != "x" || len(h.Keys()) == 0 {
		t.Error("header carrier round trip failed")
	}
}
