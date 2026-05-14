# Rinha de Backend 2026 — Go + C/AVX2

API de detecção de fraude com busca vetorial IVF usando kernels AVX2 via CGo.

![License](https://img.shields.io/github/license/macedot/rinha-2026-go?color=blue)
![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)
![C/AVX2](https://img.shields.io/badge/C-AVX2-00599C?logo=c&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)

Submissão para a [Rinha de Backend 2026](https://github.com/zanfranceschi/rinha-de-backend-2026).

## Início Rápido

```bash
docker compose up --build
```

A API escuta na porta `9999`.

## API

### `GET /health`
Retorna `200 OK` quando o servidor está pronto.

### `POST /fraud-score`

```json
{"approved": true, "fraud_score": 0.0000}
```

## Arquitetura

```
Cliente → passa (round-robin UDS) → api1 / api2
                                       ├── fasthttp (Go)
                                       ├── Vetorizador 14-dim (Go)
                                       └── Ponte IVF C/AVX2 (CGo + submódulo)
```

### Componentes

| Componente | Linguagem | Função |
|-----------|----------|--------|
| **passa** | Rust | Balanceador round-robin sobre UDS |
| **fasthttp** | Go | Servidor HTTP, listener UDS |
| **Vetorizador** | Go | 14 dimensões seguindo normalização oficial |
| **Parser JSON** | Go | Parser customizado sem alocação |
| **Ponte IVF** | C/AVX2 (CGo) | Busca vetorial via submódulo `rinha-2026-base` |

## Estrutura

```
├── cmd/
│   ├── server/main.go       # Servidor HTTP fasthttp
│   ├── build_index/main.go # Construtor do índice IVF1
│   └── bench/main.go       # Ferramenta de benchmark
├── internal/
│   ├── config.go           # Configuração via variáveis de ambiente
│   ├── httpresp.go         # Respostas HTTP pré-computadas
│   ├── ivfsearch.go        # Ponte CGo → submódulo bridge/
│   └── vectorizer.go       # Vetorizador 14-dim
├── bridge/                 # Submódulo: macedot/rinha-2026-base
├── resources/
│   ├── references.json.gz   # Base de vetores de referência
│   ├── index.bin           # Índice IVF1 gerado por build_index
│   ├── mcc_risk.json       # Tabela de risco MCC
│   └── example-*.json      # Exemplos de payloads
├── Dockerfile
├── docker-compose.yml
└── README.md
```

## Variáveis de Ambiente

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `INDEX_PATH` | `resources/index.bin` | Caminho do índice IVF |
| `IVF_NPROBE` | `8` | Clusters sondados na passada rápida |
| `IVF_FULL_NPROBE` | `24` | Clusters sondados na passada completa |
| `GOGC` | `100` | Percentual alvo do GC |
| `GOMEMLIMIT` | `100MiB` | Limite soft de memória |

## Build

```bash
# Gerar índice IVF
go run ./cmd/build_index

# Buildar servidor
go build -o server ./cmd/server

# Buildar benchmark
go build -o bench ./cmd/bench
```

## CI/CD

GitHub Actions publica imagem `ghcr.io/macedot/rinha-2026-go` a cada release.

## Licença

MIT