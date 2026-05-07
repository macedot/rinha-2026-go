<h1 align="center">Rinha de Backend 2026 — Go + C/AVX2</h1>

<p align="center"><strong>Fraud detection API using IVF vector search with hand-tuned AVX2 kernels</strong></p>

<p align="center">
  <img src="https://img.shields.io/github/license/macedot/rinha-2026-go?color=blue" alt="License" />
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/C-AVX2-00599C?logo=c&logoColor=white" alt="C/AVX2" />
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white" alt="Docker" />
  <img src="https://img.shields.io/badge/p99-27µs-4ade80" alt="p99 latency" />
</p>

---

**Submission for [Rinha de Backend 2026](https://github.com/zanfranceschi/rinha-de-backend-2026)** — fraud detection via vector search. Processes card transactions through a 14-dimensional feature vectorizer and searches 3 million reference vectors using IVF/K-means with AVX2-accelerated Euclidean distance via CGo bridge.

## Quick Start

```bash
docker compose up --build
```

The API listens on port `9999`.

### Pre-built images (from GitHub release)

```bash
IMAGE=ghcr.io/macedot/rinha-2026-go:latest docker compose up
```

Replace `build: .` with `image: ghcr.io/macedot/rinha-2026-go:latest` in `docker-compose.yml`.

## API

### `GET /ready`

Returns `200 OK` when the API has loaded the index and is ready to serve.

### `POST /fraud-score`

**Request:**
```json
{
  "id": "tx-1329056812",
  "transaction":      { "amount": 41.12, "installments": 2, "requested_at": "2026-03-11T18:45:53Z" },
  "customer":         { "avg_amount": 82.24, "tx_count_24h": 3, "known_merchants": ["MERC-003", "MERC-016"] },
  "merchant":         { "id": "MERC-016", "mcc": "5411", "avg_amount": 60.25 },
  "terminal":         { "is_online": false, "card_present": true, "km_from_home": 29.23 },
  "last_transaction": null
}
```

**Response:**
```json
{ "approved": true, "fraud_score": 0.0000 }
```

## Architecture

```
                           ┌──────────┐
                           │  Client  │
                           └─────┬────┘
                                 │ HTTP :9999
                          ┌──────▼────────┐
                          │  HAProxy 3.3  │
                          │  cpus: 0.15   │
                          │  mem:  50 MB  │
                          └───┬───────┬───┘
                              │       │
                     UDS /sockets/    UDS /sockets/
                      api1.sock       api2.sock
                   ┌──────▼──────┐ ┌──────▼──────┐
                   │    api1     │ │    api2     │
                   │ cpus: 0.425 │ │ cpus: 0.425 │
                   │ mem: 150 MB │ │ mem: 150 MB │
                   │             │ │             │
                   │┌───────────┐│ │┌───────────┐│
                   ││ fasthttp  ││ ││ fasthttp  ││
                   ││ UDS server││ ││ UDS server││
                   │└─────┬─────┘│ │└─────┬─────┘│
                   │      │      │ │      │      │
                   │┌─────▼─────┐│ │┌─────▼─────┐│
                   ││ Vectorizer││ ││ Vectorizer││
                   ││ 14-dim    ││ ││ 14-dim    ││
                   │└─────┬─────┘│ │└─────┬─────┘│
                   │      │      │ │      │      │
                   │┌─────▼─────┐│ │┌─────▼─────┐│
                   ││ C/AVX2    ││ ││ C/AVX2    ││
                   ││ IVF Search││ ││ IVF Search││
                   ││ 1024 cls. ││ ││ 1024 cls. ││
                   │└───────────┘│ │└───────────┘│
                   └─────────────┘ └─────────────┘

    ┌──────────────────────────────────────────────────────┐
    │  rinha-sockets (tmpfs, 10mb)  ·  bridge network      │
    │  CPU total: 1.0   |   Memory total: 350 MB           │
    └──────────────────────────────────────────────────────┘
```

### Request flow

1. **Client** sends `POST /fraud-score` with transaction JSON to port `9999`
2. **HAProxy** round-robin forwards the raw HTTP request over a **Unix Domain Socket** (`/sockets/api1.sock` or `api2.sock`) — zero TCP overhead, no payload inspection
3. **fasthttp** parses the JSON body (zero-allocation custom parser) and extracts all fields
4. **Vectorizer** transforms the payload into a 14-dimension float vector using the official normalization formulas
5. **C/AVX2 IVF Search (CGo bridge)** quantizes to `int16`, computes AVX2 centroid distances, selects top-N clusters, and scans AoSoA blocks with AVX2 FMA + early termination + prefetch. Returns k=5 nearest neighbors via two-stage search (fast pass → full pass for ambiguous results)
6. **fraud_score** = frauds among top 5 / 5; `approved = fraud_score < 0.6`

### Components

| Component | Language | Role |
|-----------|----------|------|
| **HAProxy 3.3** | C | Layer 7 load balancer, round-robin over UDS |
| **fasthttp server** | Go | HTTP handling, UDS listener, zero-allocation JSON parsing |
| **Vectorizer** | Go | 14-dim feature vectorizer following official normalization rules |
| **IVF Search bridge** | C/AVX2 (CGo) | IVF/K-means search: 1024 clusters, AVX2 centroid distance with FMA, AVX2 top-N cluster selection, AVX2 AoSoA block scan with early termination + prefetch, two-stage adaptive search |
| **build_index** | Go | Pre-processes `references.json.gz` (3M vectors) into IVF7 binary index: K-means clustering, `int16` quantization, transposed centroids, AoSoA block layout |

### Transport

HAProxy communicates with the API instances via **Unix Domain Sockets** on a `tmpfs` volume (`rinha-sockets`). This eliminates TCP overhead entirely — no kernel network stack, no socket buffers, no accept queues. A single 10 MB tmpfs volume holds both API socket files.

### Tech Stack

- **Go 1.24** — fasthttp HTTP server, UDS transport, custom zero-alloc JSON parser
- **C/AVX2 (CGo)** — AVX2 intrinsics for centroid distance (FMA), top-N selection, AoSoA block scan with prefetch and early termination
- **HAProxy 3.3** — stateless round-robin load balancer
- **Docker Compose** — 3 services, bridge network, resource limits via `deploy.resources.limits`

## Optimization Highlights

The IVF search kernel underwent extensive micro-optimization targeting p99 latency on a [Mac Mini Late 2014](https://support.apple.com/en-us/111931) (2.6 GHz Haswell, 8 GB RAM) with Docker resource limits of 1.0 CPU and 350 MB total memory.

| Optimization | Impact | Technique |
|-------------|--------|-----------|
| **AVX2 centroid distance** | -0.15ms p99 | Vectorized distance calc: 16 centroids/iter with FMA accumulation, transposed centroids for contiguous dim reads |
| **AVX2 top-N selection** | -0.05ms p99 | Mask-based cluster selection with 8-wide comparisons |
| **AVX2 AoSoA block scan** | -0.25ms p99 | 8-vector blocks in column-major layout, dim-pair processing with FMA, early termination after 4/7 pairs, software prefetch |
| **Two-stage search** | -0.10ms p99 | Fast pass with nprobe=8, full pass with nprobe=24 only for ambiguous results (2-3 frauds) |
| **Cluster reordering** | -0.03ms | Scan smallest clusters first to tighten worst distance sooner |
| **Transposed centroids** | -0.02ms | Column-major centroid layout for cache-friendly AVX2 loads |
| **GOGC=100, GOMEMLIMIT=100MiB** | -0.02ms | Tuned GC to avoid stop-the-world pauses under load |
| **UDS transport** | -0.08ms | HAProxy ↔ API via Unix domain sockets (zero TCP overhead) |

**Overall: ~1.56ms → ~27µs p99**

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `IVF_NPROBE` | `32` | Number of clusters probed in fast pass |
| `IVF_FULL_NPROBE` | `8` | Number of clusters probed in full pass (for ambiguous results) |
| `CANDIDATES` | `0` | Max candidates to scan (0 = unlimited) |
| `GOGC` | `100` | Go GC target percentage |
| `GOMEMLIMIT` | `100MiB` | Go soft memory limit |
| `AMOUNT_DIVISOR` | `10000` | Normalization constant (max_amount) |
| `INSTALLMENTS_DIVISOR` | `12` | Normalization constant (max_installments) |
| `TX24H_DIVISOR` | `20` | Normalization constant (max_tx_count_24h) |
| `KM_DIVISOR` | `1000` | Normalization constant (max_km) |
| `MERCHANT_AMOUNT_DIVISOR` | `10000` | Normalization constant (max_merchant_avg_amount) |

All normalization constants match the official `normalization.json`.

## Repository Structure

```
├── cmd/
│   ├── server/main.go           # fasthttp API server
│   ├── build_index/main.go      # IVF7 index builder (K-means + quantization + AoSoA packing)
│   └── bench/main.go            # Latency benchmark with p99 + instrumentation
├── internal/
│   ├── config/config.go         # Environment-based configuration
│   ├── vectorizer/vectorizer.go # 14-dim feature vectorizer
│   ├── ivfsearch/
│   │   ├── bridge.c             # C/AVX2 IVF search kernel (centroids + top-N + AoSoA scan)
│   │   ├── bridge.h             # C bridge header
│   │   ├── bridge.go            # CGo Go bindings
│   │   └── ivfsearch.go         # Shared constants
│   ├── jsonparse/jsonparse.go   # Zero-allocation JSON parser
│   ├── mccrisk/mccrisk.go       # MCC risk lookup table
│   └── httpresp/httpresp.go     # Pre-computed HTTP responses
├── resources/
│   ├── index.bin                # Pre-built IVF7 index (3M vectors, 1024 clusters, 84MB)
│   ├── mcc_risk.json            # Merchant category risk table
│   ├── references.json.gz       # 3M labeled reference vectors (input to build_index)
│   ├── example-payloads.json    # Example transaction payloads
│   └── example-references.json  # Example reference vectors
├── Dockerfile                   # Multi-stage: Go build → slim Debian runtime
├── docker-compose.yml           # 3-service deployment with resource limits
├── haproxy.cfg                  # HAProxy round-robin UDS configuration
├── .github/workflows/release.yml # CI: build & push Docker image to GHCR
├── LICENSE                      # MIT
├── info.json                    # Rinha participant info
└── README.md
```

> The `submission` branch contains only `docker-compose.yml`, `haproxy.cfg`, and `info.json` — no source code. It references the pre-built `ghcr.io/macedot/rinha-2026-go:latest` image.

## CI/CD

GitHub Actions builds and pushes a `linux/amd64` Docker image to `ghcr.io/macedot/rinha-2026-go` on every published release (prereleases excluded). Images are tagged with both the release version and `latest`.

## Test Environment

The official test runs on a Mac Mini Late 2014 (2.6 GHz Haswell, 8 GB RAM, Ubuntu 24.04) with Docker resource limits of **1.0 CPU** and **350 MB memory** across all services. All optimizations were tuned specifically for this hardware.

## License

This project is licensed under the [MIT License](LICENSE).
