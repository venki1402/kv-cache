package main

import (
	"container/list"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
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

	for shard.currentSize > shard.maxSize && shard.evictList.Len() > 0 {
		backElem := shard.evictList.Back()
		if backElem != nil {
			evictEntry := backElem.Value.(*CacheEntry)

			delete(shard.items, evictEntry.key)
			shard.evictList.Remove(backElem)
			shard.currentSize -= evictEntry.size
		}
	}

	return nil
}

func (c *ShardedCache) Get(key string) (string, bool) {
	shard := c.getShard(key)

	shard.lock.Lock()
	defer shard.lock.Unlock()

	if element, found := shard.items[key]; found {
		shard.evictList.MoveToFront(element)
		return element.Value.(*CacheEntry).value, true
	}

	return "", false
}

var cache *ShardedCache

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
)

func requestHandler(ctx *fasthttp.RequestCtx) {
	ctx.Response.Header.Set("Content-Type", "application/json")

	switch string(ctx.Method()) {
	case "POST", "PUT":
		if string(ctx.Path()) == "/put" {
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

			if err := cache.Put(req.Key, req.Value); err != nil {
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
		if string(ctx.Path()) == "/get" {
			key := string(ctx.QueryArgs().Peek("key"))
			if key == "" {
				ctx.SetStatusCode(fasthttp.StatusBadRequest)
				ctx.SetBody(keyParamReqBytes)
				return
			}

			if value, found := cache.Get(key); found {
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
	runtime.GOMAXPROCS(runtime.NumCPU())
	cache = NewShardedCache(NumShards, MaxMemoryBytes)

	s := &fasthttp.Server{
		Handler:              requestHandler,
		Name:                 "kv-cache",
		ReadTimeout:          5 * time.Second,
		WriteTimeout:         5 * time.Second,
		MaxConnsPerIP:        0,
		MaxKeepaliveDuration: 60 * time.Second,
	}

	log.Printf("Starting kv-cache server on :7171")
	if err := s.ListenAndServe(":7171"); err != nil {
		log.Fatalf("Server Error: %s", err)
	}
}
