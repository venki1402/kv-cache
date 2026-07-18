package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/valyala/fasthttp"
)

func TestShardedCache_PutAndGet(t *testing.T) {
	sc := NewShardedCache(4, 1024*1024)

	sc.Put("key1", "value1")
	val, ok := sc.Get("key1")

	if !ok {
		t.Fatal("expected key1 to exist")
	}
	if val != "value1" {
		t.Fatalf("expected value1, got %s", val)
	}
}

func TestShardedCache_Update(t *testing.T) {
	sc := NewShardedCache(4, 1024*1024)

	sc.Put("key1", "value1")
	sc.Put("key1", "value2")

	val, ok := sc.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist")
	}
	if val != "value2" {
		t.Fatalf("expected value2, got %s", val)
	}
}

func TestDjb2Hash(t *testing.T) {
	hash1 := djb2Hash("test")
	hash2 := djb2Hash("test")

	if hash1 != hash2 {
		t.Fatal("same input should produce same hash")
	}

	hash3 := djb2Hash("different")
	if hash1 == hash3 {
		t.Fatal("different inputs should produce different hashes")
	}
}

func TestShardedCache_LRUEviction(t *testing.T) {
	sc := NewShardedCache(1, 200)

	sc.Put("k1", "v1")
	sc.Put("k2", "v2")
	sc.Put("k3", "v3")

	if _, ok := sc.Get("k1"); ok {
		t.Fatal("expected k1 to be evicted due to memory limits")
	}
	if _, ok := sc.Get("k2"); !ok {
		t.Fatal("expected k2 to survive")
	}
	if _, ok := sc.Get("k3"); !ok {
		t.Fatal("expected k3 to survive")
	}
}

func TestRequestHandler_Put(t *testing.T) {
	cache = NewShardedCache(4, 1024*1024)

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod("POST")
	ctx.Request.SetRequestURI("/put")
	ctx.Request.SetBody([]byte(`{"key":"testkey","value":"testvalue"}`))

	requestHandler(ctx)

	if ctx.Response.StatusCode() != 200 {
		t.Fatalf("expected 200, got %d", ctx.Response.StatusCode())
	}

	val, ok := cache.Get("testkey")
	if !ok {
		t.Fatal("expected testkey to exist in cache")
	}
	if val != "testvalue" {
		t.Fatalf("expected testvalue, got %s", val)
	}
}

func TestRequestHandler_Get(t *testing.T) {
	cache = NewShardedCache(4, 1024*1024)
	cache.Put("testkey", "testvalue")

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod("GET")
	ctx.Request.SetRequestURI("/get?key=testkey")

	requestHandler(ctx)

	if ctx.Response.StatusCode() != 200 {
		t.Fatalf("expected 200, got %d", ctx.Response.StatusCode())
	}

	var resp GetResponse
	if err := json.Unmarshal(ctx.Response.Body(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Status != "OK" {
		t.Fatalf("expected OK status, got %s", resp.Status)
	}
	if resp.Value != "testvalue" {
		t.Fatalf("expected testvalue, got %s", resp.Value)
	}
}

func TestRequestHandler_Health(t *testing.T) {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod("GET")
	ctx.Request.SetRequestURI("/health")

	requestHandler(ctx)

	if ctx.Response.StatusCode() != 200 {
		t.Fatalf("expected 200, got %d", ctx.Response.StatusCode())
	}

	var resp map[string]string
	if err := json.Unmarshal(ctx.Response.Body(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp["status"] != "healthy" {
		t.Fatalf("expected healthy status, got %s", resp["status"])
	}
}

func TestRequestHandler_PutBadJSON(t *testing.T) {
	cache = NewShardedCache(4, 1024*1024)

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod("POST")
	ctx.Request.SetRequestURI("/put")
	ctx.Request.SetBody([]byte(`{invalid json}`))

	requestHandler(ctx)

	if ctx.Response.StatusCode() != 400 {
		t.Fatalf("expected 400, got %d", ctx.Response.StatusCode())
	}
}

func TestRequestHandler_PutKeyTooLong(t *testing.T) {
	cache = NewShardedCache(4, 1024*1024)

	longKey := string(make([]byte, 300))
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod("POST")
	ctx.Request.SetRequestURI("/put")
	ctx.Request.SetBody([]byte(fmt.Sprintf(`{"key":"%s","value":"v"}`, longKey)))

	requestHandler(ctx)

	if ctx.Response.StatusCode() != 400 {
		t.Fatalf("expected 400, got %d", ctx.Response.StatusCode())
	}
}
