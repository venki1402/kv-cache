package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

// Route labels are a fixed set so metric cardinality stays bounded.
const (
	routeGet = iota
	routePut
	routeHealth
	routeHealthz
	routeReadyz
	routeVersion
	routeOther
	numRoutes
)

var routeNames = [numRoutes]string{"get", "put", "health", "healthz", "readyz", "version", "other"}

func routeOf(path []byte) int {
	switch string(path) {
	case "/get":
		return routeGet
	case "/put":
		return routePut
	case "/health":
		return routeHealth
	case "/healthz":
		return routeHealthz
	case "/readyz":
		return routeReadyz
	case "/version":
		return routeVersion
	}
	return routeOther
}

func methodLabel(m []byte) string {
	switch string(m) {
	case "GET":
		return "GET"
	case "POST":
		return "POST"
	case "PUT":
		return "PUT"
	}
	return "OTHER"
}

var (
	registry = prometheus.NewRegistry()

	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kvcache_http_requests_total",
		Help: "HTTP requests by route, method and status code.",
	}, []string{"route", "method", "code"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kvcache_http_request_duration_seconds",
		Help:    "HTTP request latency by route.",
		Buckets: []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"route"})

	cacheHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kvcache_cache_hits_total",
		Help: "Cache lookups that found the key.",
	})
	cacheMisses = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kvcache_cache_misses_total",
		Help: "Cache lookups that did not find the key.",
	})
	cacheEvictions = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kvcache_evictions_total",
		Help: "Entries evicted to stay under the memory ceiling.",
	})
	maxBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "kvcache_max_bytes",
		Help: "Configured memory ceiling for cached data.",
	})
	buildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kvcache_build_info",
		Help: "Build metadata of the running binary; always 1.",
	}, []string{"version", "commit"})

	// Resolved once per route to avoid a label lookup on every request.
	durationByRoute [numRoutes]prometheus.Observer
)

func init() {
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		httpRequests, httpDuration, cacheHits, cacheMisses, cacheEvictions, maxBytes, buildInfo,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "kvcache_bytes_used",
			Help: "Bytes accounted to cached entries.",
		}, func() float64 { b, _ := cacheStats(); return float64(b) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "kvcache_keys",
			Help: "Number of keys currently cached.",
		}, func() float64 { _, k := cacheStats(); return float64(k) }),
	)
	buildInfo.WithLabelValues(version, commit).Set(1)
	for i := range durationByRoute {
		durationByRoute[i] = httpDuration.WithLabelValues(routeNames[i])
	}
}

// Locks every shard, so it is only called at scrape time.
func cacheStats() (bytes int64, keys int) {
	c := cache
	if c == nil {
		return 0, 0
	}
	return c.Stats()
}

func metricsHandler() fasthttp.RequestHandler {
	h := fasthttpadaptor.NewFastHTTPHandler(promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	return func(ctx *fasthttp.RequestCtx) {
		if string(ctx.Path()) != "/metrics" {
			ctx.SetStatusCode(fasthttp.StatusNotFound)
			return
		}
		h(ctx)
	}
}

var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func setupLogger(level slog.Level) {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).
		With("version", version, "commit", commit)
}

// Never logs keys, values or headers: they may hold user data or the token.
func instrument(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		start := time.Now()
		ctx.Response.Header.Set("X-App-Version", version)

		next(ctx)

		elapsed := time.Since(start)
		route := routeOf(ctx.Path())
		code := ctx.Response.StatusCode()
		method := methodLabel(ctx.Method())
		httpRequests.WithLabelValues(routeNames[route], method, strconv.Itoa(code)).Inc()
		durationByRoute[route].Observe(elapsed.Seconds())

		if logger.Enabled(context.Background(), slog.LevelDebug) {
			attrs := []slog.Attr{
				slog.String("route", routeNames[route]),
				slog.String("method", method),
				slog.Int("status", code),
				slog.Float64("duration_ms", float64(elapsed.Microseconds())/1000),
			}
			if id := traceIDFromCtx(ctx); id != "" {
				attrs = append(attrs, slog.String("trace_id", id))
			}
			logger.LogAttrs(context.Background(), slog.LevelDebug, "request", attrs...)
		}
	}
}
