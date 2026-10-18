package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valyala/fasthttp"
)

func doRequest(h fasthttp.RequestHandler, method, uri, body string, headers map[string]string) *fasthttp.RequestCtx {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod(method)
	ctx.Request.SetRequestURI(uri)
	for k, v := range headers {
		ctx.Request.Header.Set(k, v)
	}
	if body != "" {
		ctx.Request.SetBody([]byte(body))
	}
	h(ctx)
	return ctx
}

func TestHealthz(t *testing.T) {
	ctx := doRequest(requestHandler, "GET", "/healthz", "", nil)
	if ctx.Response.StatusCode() != 200 {
		t.Fatalf("expected 200, got %d", ctx.Response.StatusCode())
	}
}

func TestReadyz(t *testing.T) {
	defer ready.Store(false)
	defer func() { failReadiness = false }()

	ready.Store(false)
	if c := doRequest(requestHandler, "GET", "/readyz", "", nil).Response.StatusCode(); c != 503 {
		t.Fatalf("expected 503 before init, got %d", c)
	}
	ready.Store(true)
	if c := doRequest(requestHandler, "GET", "/readyz", "", nil).Response.StatusCode(); c != 200 {
		t.Fatalf("expected 200 when ready, got %d", c)
	}
	failReadiness = true
	if c := doRequest(requestHandler, "GET", "/readyz", "", nil).Response.StatusCode(); c != 503 {
		t.Fatalf("expected 503 with FAIL_READINESS, got %d", c)
	}
}

func TestVersion(t *testing.T) {
	ctx := doRequest(instrument(requestHandler), "GET", "/version", "", nil)
	if ctx.Response.StatusCode() != 200 {
		t.Fatalf("expected 200, got %d", ctx.Response.StatusCode())
	}
	var v map[string]string
	if err := json.Unmarshal(ctx.Response.Body(), &v); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if v["version"] != version || v["commit"] != commit || v["buildTime"] != buildTime {
		t.Fatalf("unexpected version body: %v", v)
	}
	if got := string(ctx.Response.Header.Peek("X-App-Version")); got != version {
		t.Fatalf("expected X-App-Version %q, got %q", version, got)
	}
}

func TestAuth(t *testing.T) {
	cache = NewShardedCache(4, 1024*1024)
	authToken = "s3cret"
	defer func() { authToken = "" }()
	body := `{"key":"k","value":"v"}`

	if c := doRequest(requestHandler, "POST", "/put", body, nil).Response.StatusCode(); c != 401 {
		t.Fatalf("expected 401 without token, got %d", c)
	}
	if c := doRequest(requestHandler, "POST", "/put", body,
		map[string]string{"Authorization": "Bearer wrong"}).Response.StatusCode(); c != 401 {
		t.Fatalf("expected 401 with wrong token, got %d", c)
	}
	if c := doRequest(requestHandler, "POST", "/put", body,
		map[string]string{"Authorization": "Bearer s3cret"}).Response.StatusCode(); c != 200 {
		t.Fatalf("expected 200 with token, got %d", c)
	}
	if c := doRequest(requestHandler, "GET", "/get?key=k", "", nil).Response.StatusCode(); c != 200 {
		t.Fatalf("expected 200 on get, got %d", c)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	cache = NewShardedCache(1, 200)
	h := instrument(requestHandler)
	doRequest(h, "POST", "/put", `{"key":"k1","value":"v1"}`, nil)
	doRequest(h, "POST", "/put", `{"key":"k2","value":"v2"}`, nil)
	doRequest(h, "POST", "/put", `{"key":"k3","value":"v3"}`, nil) // evicts k1
	doRequest(h, "GET", "/get?key=k3", "", nil)
	doRequest(h, "GET", "/get?key=missing", "", nil)
	doRequest(h, "GET", "/some/raw/path", "", nil)

	ctx := doRequest(metricsHandler(), "GET", "/metrics", "", nil)
	if ctx.Response.StatusCode() != 200 {
		t.Fatalf("expected 200, got %d", ctx.Response.StatusCode())
	}
	out := string(ctx.Response.Body())
	for _, want := range []string{
		`kvcache_http_requests_total{code="200",method="POST",route="put"}`,
		`kvcache_http_requests_total{code="404",method="GET",route="other"}`,
		`kvcache_http_request_duration_seconds_bucket{route="get",le="0.05"}`,
		"kvcache_cache_hits_total",
		"kvcache_cache_misses_total",
		"kvcache_evictions_total",
		"kvcache_bytes_used",
		"kvcache_keys 2",
		`kvcache_build_info{commit="` + commit + `",version="` + version + `"} 1`,
		"go_goroutines",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
	if strings.Contains(out, "/some/raw/path") {
		t.Error("raw path leaked into metric labels")
	}

	if c := doRequest(metricsHandler(), "GET", "/other", "", nil).Response.StatusCode(); c != 404 {
		t.Fatalf("expected 404 for non-metrics path, got %d", c)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	for _, k := range []string{"PORT", "METRICS_PORT", "MAX_MEMORY_BYTES", "AUTH_TOKEN", "LOG_LEVEL",
		"OTEL_ENABLED", "OTEL_TRACES_SAMPLER_ARG", "SHUTDOWN_DELAY", "FAIL_READINESS", "MEMORY_LIMIT_BYTES"} {
		t.Setenv(k, "")
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "7171" || cfg.MetricsPort != "9091" || cfg.MaxMemoryBytes != MaxMemoryBytes ||
		cfg.AuthToken != "" || cfg.OTelEnabled || cfg.FailReadiness {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadConfigOverridesAndErrors(t *testing.T) {
	t.Setenv("MAX_MEMORY_BYTES", "134217728")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("OTEL_ENABLED", "true")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.1")
	t.Setenv("SHUTDOWN_DELAY", "1s")
	t.Setenv("GOMEMLIMIT", "")
	t.Setenv("MEMORY_LIMIT_BYTES", "1000")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxMemoryBytes != 134217728 || !cfg.OTelEnabled || cfg.OTelSampleRate != 0.1 ||
		cfg.ShutdownDelay != time.Second || cfg.LogLevel.String() != "DEBUG" || cfg.SoftMemoryLimit != 900 {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	for k, v := range map[string]string{
		"MAX_MEMORY_BYTES":        "lots",
		"LOG_LEVEL":               "loud",
		"OTEL_ENABLED":            "maybe",
		"OTEL_TRACES_SAMPLER_ARG": "2",
		"SHUTDOWN_DELAY":          "soon",
		"MEMORY_LIMIT_BYTES":      "0",
	} {
		t.Run(k, func(t *testing.T) {
			t.Setenv(k, v)
			if _, err := loadConfig(); err == nil {
				t.Fatalf("expected error for %s=%s", k, v)
			}
		})
	}
}

// Meant to run under -race.
func TestConcurrentPutGet(t *testing.T) {
	cache = NewShardedCache(NumShards, 64*1024)
	h := instrument(requestHandler)
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				key := fmt.Sprintf("g%d-k%d", g, i%50)
				put := doRequest(h, "POST", "/put", fmt.Sprintf(`{"key":%q,"value":"v%d"}`, key, i), nil)
				if put.Response.StatusCode() != 200 {
					t.Errorf("put failed: %d", put.Response.StatusCode())
					return
				}
				get := doRequest(h, "GET", "/get?key="+key, "", nil)
				if c := get.Response.StatusCode(); c != 200 && c != 404 {
					t.Errorf("unexpected get status %d", c)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	if b, _ := cache.Stats(); b > 64*1024 {
		t.Fatalf("memory ceiling exceeded: %d", b)
	}
}
