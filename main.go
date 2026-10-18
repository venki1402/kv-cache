package main

import (
	"container/list"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	NumShards      = 32
	MaxKeySize     = 256
	MaxValueSize   = 256
	MaxMemoryBytes = 1.0 * 1024 * 1024 * 1024
)

type CacheEntry struct {
	key   string
	value string
	size  int64
}

type Shard struct {
	items       map[string]*list.Element
	evictList   *list.List
	lock        sync.Mutex
	currentSize int64
	maxSize     int64
}

type ShardedCache struct {
	shards    []*Shard
	shardMask uint64
}

type PutRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type GetResponse struct {
	Status string `json:"status"`
	Key    string `json:"key,omitempty"`
	Value  string `json:"value,omitempty"`
}

func NewShardedCache(numShards int, maxTotalMemory int64) *ShardedCache {
	powerOf2 := 1
	for powerOf2 < numShards {
		powerOf2 *= 2
	}

	shardMemoryLimit := maxTotalMemory / int64(powerOf2)

	sc := &ShardedCache{
		shards:    make([]*Shard, powerOf2),
		shardMask: uint64(powerOf2 - 1),
	}

	for i := 0; i < powerOf2; i++ {
		sc.shards[i] = &Shard{
			items:       make(map[string]*list.Element, 10000),
			evictList:   list.New(),
			maxSize:     shardMemoryLimit,
			currentSize: 0,
		}
	}
	return sc
}

func djb2Hash(s string) uint64 {
	var hash uint64 = 5381
	for i := 0; i < len(s); i++ {
		hash = ((hash << 5) + hash) + uint64(s[i])
	}
	return hash
}

func (c *ShardedCache) getShard(key string) *Shard {
	return c.shards[djb2Hash(key)&c.shardMask]
}

func (c *ShardedCache) Put(key, value string) error {
	return c.put(context.Background(), key, value)
}

// ctx is only used to parent the eviction span when tracing is enabled.
func (c *ShardedCache) put(ctx context.Context, key, value string) error {
	if len(key) > MaxKeySize || len(value) > MaxValueSize {
		return fmt.Errorf("key or value exceeds maximum size")
	}

	entrySize := int64(len(key) + len(value) + 64)
	shard := c.getShard(key)

	shard.lock.Lock()
	defer shard.lock.Unlock()

	if element, found := shard.items[key]; found {
		entry := element.Value.(*CacheEntry)

		shard.currentSize -= entry.size
		shard.currentSize += entrySize

		entry.value = value
		entry.size = entrySize
		shard.evictList.MoveToFront(element)
	} else {
		entry := &CacheEntry{
			key:   key,
			value: value,
			size:  entrySize,
		}
		element := shard.evictList.PushFront(entry)
		shard.items[key] = element
		shard.currentSize += entrySize
	}

	var evictSpan trace.Span
	evicted := 0
	for shard.currentSize > shard.maxSize && shard.evictList.Len() > 0 {
		if evictSpan == nil && tracingEnabled {
			_, evictSpan = tracer.Start(ctx, "cache.evict")
		}
		backElem := shard.evictList.Back()
		if backElem != nil {
			evictEntry := backElem.Value.(*CacheEntry)

			delete(shard.items, evictEntry.key)
			shard.evictList.Remove(backElem)
			shard.currentSize -= evictEntry.size
			evicted++
		}
	}
	if evicted > 0 {
		cacheEvictions.Add(float64(evicted))
	}
	if evictSpan != nil {
		evictSpan.SetAttributes(attribute.Int("cache.evicted", evicted))
		evictSpan.End()
	}

	return nil
}

func (c *ShardedCache) Get(key string) (string, bool) {
	shard := c.getShard(key)

	shard.lock.Lock()
	defer shard.lock.Unlock()

	if element, found := shard.items[key]; found {
		shard.evictList.MoveToFront(element)
		cacheHits.Inc()
		return element.Value.(*CacheEntry).value, true
	}

	cacheMisses.Inc()
	return "", false
}

func (c *ShardedCache) Stats() (bytes int64, keys int) {
	for _, shard := range c.shards {
		shard.lock.Lock()
		bytes += shard.currentSize
		keys += len(shard.items)
		shard.lock.Unlock()
	}
	return bytes, keys
}

func (c *ShardedCache) shardIndex(key string) int {
	return int(djb2Hash(key) & c.shardMask)
}

var (
	cache *ShardedCache

	authToken string
	ready     atomic.Bool
	// Only for fault drills: lets a release fail readiness on purpose.
	failReadiness bool

	versionBytes []byte
)

// Pre-marshaled responses for hot/static paths — avoids per-request
// allocation and formatting (fmt.Fprintf / json.Marshal) on responses
// whose bodies never change.
var (
	putSuccessBytes   = []byte(`{"status":"OK","message":"Key inserted/updated"}`)
	invalidJSONBytes  = []byte(`{"status":"ERROR","message":"Invalid JSON"}`)
	keyRequiredBytes  = []byte(`{"status":"ERROR","message":"Key is required"}`)
	keyParamReqBytes  = []byte(`{"status":"ERROR","message":"Key parameter is required"}`)
	sizeExceededBytes = []byte(`{"status":"ERROR","message":"key or value exceeds maximum size"}`)
	keyNotFoundBytes  = []byte(`{"status":"ERROR","message":"Key not found"}`)
	notFoundBytes     = []byte(`{"status":"ERROR","message":"Endpoint not found"}`)
	healthOKBytes     = []byte(`{"status":"healthy"}`)
	unauthorizedBytes = []byte(`{"status":"ERROR","message":"Unauthorized"}`)
	readyBytes        = []byte(`{"status":"ready"}`)
	notReadyBytes     = []byte(`{"status":"not ready"}`)
)

func init() {
	versionBytes, _ = json.Marshal(map[string]string{
		"version":   version,
		"commit":    commit,
		"buildTime": buildTime,
	})
}

// Only writes are protected. Reads are side-effect free, the Service is
// ClusterIP behind a NetworkPolicy, and an extra header check would cost
// latency on the hottest path.
func authorized(ctx *fasthttp.RequestCtx) bool {
	if authToken == "" {
		return true
	}
	got := ctx.Request.Header.Peek("Authorization")
	want := "Bearer " + authToken
	return subtle.ConstantTimeCompare(got, []byte(want)) == 1
}

func cacheSpan(ctx *fasthttp.RequestCtx, op, key string) (context.Context, trace.Span) {
	if !tracingEnabled {
		return context.Background(), nil
	}
	return tracer.Start(requestContext(ctx), "cache."+op,
		trace.WithAttributes(attribute.Int("cache.shard", cache.shardIndex(key))))
}

func requestHandler(ctx *fasthttp.RequestCtx) {
	ctx.Response.Header.Set("Content-Type", "application/json")

	switch string(ctx.Method()) {
	case "POST", "PUT":
		if string(ctx.Path()) == "/put" {
			if !authorized(ctx) {
				ctx.SetStatusCode(fasthttp.StatusUnauthorized)
				ctx.SetBody(unauthorizedBytes)
				return
			}

			var req PutRequest
			if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
				ctx.SetStatusCode(fasthttp.StatusBadRequest)
				ctx.SetBody(invalidJSONBytes)
				return
			}

			if req.Key == "" {
				ctx.SetStatusCode(fasthttp.StatusBadRequest)
				ctx.SetBody(keyRequiredBytes)
				return
			}

			spanCtx, span := cacheSpan(ctx, "put", req.Key)
			err := cache.put(spanCtx, req.Key, req.Value)
			if span != nil {
				span.End()
			}
			if err != nil {
				ctx.SetStatusCode(fasthttp.StatusBadRequest)
				ctx.SetBody(sizeExceededBytes)
				return
			}

			ctx.SetStatusCode(fasthttp.StatusOK)
			ctx.SetBody(putSuccessBytes)
			return
		}

	case "GET":
		if string(ctx.Path()) == "/health" {
			ctx.SetStatusCode(fasthttp.StatusOK)
			ctx.SetBody(healthOKBytes)
			return
		}
		if string(ctx.Path()) == "/healthz" {
			ctx.SetStatusCode(fasthttp.StatusOK)
			ctx.SetBody(healthOKBytes)
			return
		}
		if string(ctx.Path()) == "/readyz" {
			if ready.Load() && !failReadiness {
				ctx.SetStatusCode(fasthttp.StatusOK)
				ctx.SetBody(readyBytes)
			} else {
				ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
				ctx.SetBody(notReadyBytes)
			}
			return
		}
		if string(ctx.Path()) == "/version" {
			ctx.SetStatusCode(fasthttp.StatusOK)
			ctx.SetBody(versionBytes)
			return
		}
		if string(ctx.Path()) == "/get" {
			key := string(ctx.QueryArgs().Peek("key"))
			if key == "" {
				ctx.SetStatusCode(fasthttp.StatusBadRequest)
				ctx.SetBody(keyParamReqBytes)
				return
			}

			_, span := cacheSpan(ctx, "get", key)
			value, found := cache.Get(key)
			if span != nil {
				span.SetAttributes(attribute.Bool("cache.hit", found))
				span.End()
			}
			if found {
				ctx.SetStatusCode(fasthttp.StatusOK)

				respJSON, _ := json.Marshal(GetResponse{
					Status: "OK",
					Key:    key,
					Value:  value,
				})
				ctx.SetBody(respJSON)
			} else {
				ctx.SetStatusCode(fasthttp.StatusNotFound)
				ctx.SetBody(keyNotFoundBytes)
			}
			return
		}
	}

	ctx.SetStatusCode(fasthttp.StatusNotFound)
	ctx.SetBody(notFoundBytes)
}

func main() {
	if err := run(); err != nil {
		logger.Error("fatal", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	setupLogger(cfg.LogLevel)

	runtime.GOMAXPROCS(runtime.NumCPU())
	if cfg.SoftMemoryLimit > 0 {
		debug.SetMemoryLimit(cfg.SoftMemoryLimit)
	}
	cache = NewShardedCache(NumShards, cfg.MaxMemoryBytes)
	maxBytes.Set(float64(cfg.MaxMemoryBytes))
	authToken = cfg.AuthToken
	failReadiness = cfg.FailReadiness

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	handler := instrument(requestHandler)
	shutdownTracing := func(context.Context) error { return nil }
	if cfg.OTelEnabled {
		shutdownTracing, err = setupTracing(ctx, cfg.OTelSampleRate)
		if err != nil {
			return fmt.Errorf("tracing setup: %w", err)
		}
		handler = traced(handler)
	}

	s := &fasthttp.Server{
		Handler:       handler,
		Name:          "kv-cache",
		ReadTimeout:   5 * time.Second,
		WriteTimeout:  5 * time.Second,
		MaxConnsPerIP: 0,
	}
	ms := &fasthttp.Server{
		Handler:      metricsHandler(),
		Name:         "kv-cache-metrics",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() { errCh <- ms.ListenAndServe(":" + cfg.MetricsPort) }()
	go func() { errCh <- s.ListenAndServe(":" + cfg.Port) }()

	ready.Store(true)
	logger.Info("starting kv-cache server",
		"port", cfg.Port,
		"metrics_port", cfg.MetricsPort,
		"max_memory_bytes", cfg.MaxMemoryBytes,
		"auth_enabled", cfg.AuthToken != "",
		"tracing_enabled", cfg.OTelEnabled,
		"build_time", buildTime)

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
	}

	// Stop receiving new traffic first, then drain in-flight requests.
	ready.Store(false)
	logger.Info("shutdown started", "delay", cfg.ShutdownDelay.String())
	time.Sleep(cfg.ShutdownDelay)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	errs := []error{
		s.ShutdownWithContext(shutdownCtx),
		ms.ShutdownWithContext(shutdownCtx),
		shutdownTracing(shutdownCtx),
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("shutdown complete")
	return nil
}
