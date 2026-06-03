# ultra fast sharded key value lru cache

![Go Version](https://img.shields.io/badge/Go-1.24-blue.svg)
![Coverage](https://img.shields.io/badge/Coverage-76.5%25-brightgreen.svg)
![Build](https://img.shields.io/badge/Build-Passing-brightgreen.svg)

a high throughput, memory-bounded, sharded LRU key-value cache written in Go. uses bitwise request routing across 32 shards to minimize lock contention and deterministic O(1) eviction to maintain a strict memory limit, sustaining **172K+ requests/sec** at **~3.2 ms median** latency..

## performance benchmarks

try it yourself - https://gist.github.com/Venki1402/4c9c0e99106ede89a558c04768a56e3d

benchmarked using `wrk` and a `Lua` randomization script to simulate 400 concurrent connections across 8 threads for 30 seconds.

* **Throughput**: 172, 760 Requests / Second
* **Average Latency**: 3.25 ms
* **Total Volume**: 5,198,917 requests served in 30 seconds
* **Data Transferred**: 1.00 GB

```text
Running 30s test @ http://localhost:7171
  8 threads and 400 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     3.25ms    8.15ms 153.97ms   92.83%
    Req/Sec    21.79k     7.82k   62.17k    72.32%
  5198917 requests in 30.09s, 0.99GB read
Requests/sec: 172760.78
Transfer/sec:     33.58MB
```

## architecture highlights

* Zero Lock Contention: Utilizes a 32-way sharded architecture. Requests are routed to specific shards using a highly optimized djb2 bitwise hashing algorithm, ensuring that parallel requests rarely fight for the same sync.Mutex.

* Deterministic O(1) Eviction: Implements a strict doubly-linked list (LRU) combined with a map pointer system. When a shard hits its math-allocated memory threshold, it evicts stale data in true O(1) time.

* Strict Memory Bounding: Tracks exact byte allocations per shard instead of relying on standard runtime.MemStats. This keeps the cache footprint strictly under the 1GB threshold without triggering "stop-the-world" Go GC pauses.

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

The project maintains 76.5% test coverage, specifically targeting core sharding logic, hash collisions, O(1) memory eviction constraints, and HTTP edge cases.

> To run the test suite:

```Bash
go test -v -cover
```

## License

This project is licensed under the MIT License.
