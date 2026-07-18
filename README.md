# ultra fast sharded key value lru cache

![Go Version](https://img.shields.io/badge/Go-1.24-blue.svg)
![Coverage](https://img.shields.io/badge/Coverage-77.5%25-brightgreen.svg)
![Build](https://img.shields.io/badge/Build-Passing-brightgreen.svg)

a high throughput, memory-bounded, sharded LRU key-value cache written in Go. uses bitwise request routing across 32 shards to minimize lock contention and deterministic O(1) eviction to maintain a strict memory limit, sustaining **172K+ requests/sec** at **~2.15 ms median (p50)** latency.

## performance benchmarks

try it yourself - https://gist.github.com/Venki1402/4c9c0e99106ede89a558c04768a56e3d

## instructions to benchmark venki1402/kv-cache yourself

### run it
```bash
docker run -p 7171:7171 venki1402/kv-cache
```

#### seed the cache with 1,000 keys

```bash
for i in {1..1000}; do
  curl -X POST http://localhost:7171/put -H "Content-Type: application/json" -d "{\\"key\\":\\"key$i\\", \\"value\\":\\"This is the cached value for item $i\\"}"
done
```

#### create the `random_keys.lua` script

```lua
math.randomseed(os.time())
request = function()
   local random_id = math.random(1, 1000)
   local path = "/get?key=key" .. random_id
   return wrk.format("GET", path)
end
```

#### run `wrk`

```bash
wrk -t8 -c400 -d30s --latency -s random_keys.lua http://localhost:7171
```

#### results

```
Running 30s test @ http://localhost:7171
  8 threads and 400 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     2.15ms  817.35us  38.41ms   90.43%
    Req/Sec    21.77k     1.35k   33.01k    79.08%
  Latency Distribution
     50%    2.15ms
     75%    2.28ms
     90%    2.49ms
     99%    4.41ms
  5205733 requests in 30.10s, 0.99GB read
Requests/sec: 172965.97
Transfer/sec:     33.62MB
```

benchmarked using `wrk` and a `Lua` randomization script to simulate 400 concurrent connections across 8 threads for 30 seconds. the workload is read-heavy: the script issues only `GET` requests against 1,000 pre-seeded keys (near-100% cache hits).

* **Throughput**: 172,965 Requests / Second
* **Median Latency (p50)**: 2.15 ms
* **p99 Latency**: 4.41 ms
* **Total Volume**: 5,205,733 requests served in 30 seconds
* **Data Transferred**: 0.99 GB

## architecture highlights

* Minimized Lock Contention: Utilizes a 32-way sharded architecture. Requests are routed to specific shards using a djb2 bitwise hashing algorithm, so parallel requests rarely fight for the same sync.Mutex.

* Deterministic O(1) Eviction: Implements a strict doubly-linked list (LRU) combined with a map pointer system. When a shard hits its math-allocated memory threshold, it evicts stale data in true O(1) time.

* Strict Memory Bounding: Tracks exact byte allocations per shard instead of relying on standard runtime.MemStats, keeping the cached data strictly under a configurable 1GB ceiling.

* High-Speed HTTP: Powered by fasthttp, the fastest HTTP package available for Go, bypassing the standard library's overhead for extreme raw throughput.

## local development

> prerequisites

Go 1.21 or higher

> clone the repository

```bash
git clone https://github.com/venki1402/kv-cache
cd kv-cache
```

> install dependencies

```bash
go mod tidy
```

> run the server

```bash
go run main.go
```

**The server will start on localhost:7171**

## run with docker

> build the docker image

```bash
docker build -t kv-cache .
```

> run the container

```bash
docker run -p 7171:7171 kv-cache
```

**The server will start on localhost:7171**

## api usage

### 1. Insert / Update a Key (PUT)

```Bash
curl -X POST <http://localhost:7171/put> \\
  -H "Content-Type: application/json" \\
  -d '{"key":"king", "value":"venki"}'
```

Response:

```JSON
{"status":"OK","message":"Key inserted/updated"}
```

### 2. Retrieve a Key (GET)

```Bash
curl -X GET "<http://localhost:7171/get?key=king>"
```

Response:

```JSON
{"status":"OK","key":"king","value":"venki"}
```

## testing

The project maintains 77.5% test coverage, specifically targeting core sharding logic, hash collisions, O(1) memory eviction constraints, and HTTP edge cases.

> To run the test suite:

```Bash
go test -v -cover
```

## License

This project is licensed under the MIT License.
