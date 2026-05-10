<h1 align="center">Rinha de Backend 2026 — Go + C/AVX2</h1>

<p align="center"><strong>API de detecção de fraude com busca vetorial IVF usando kernels AVX2 via CGo</strong></p>

<p align="center">
  <img src="https://img.shields.io/github/license/macedot/rinha-2026-go?color=blue" alt="License" />
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/C-AVX2-00599C?logo=c&logoColor=white" alt="C/AVX2" />
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white" alt="Docker" />
</p>

---

**Submissão para a [Rinha de Backend 2026](https://github.com/zanfranceschi/rinha-de-backend-2026).** Servidor HTTP em Go (fasthttp), vetorizador e parser JSON nativos, ponte de busca vetorial IVF/AVX2 via CGo e submódulo [`rinha-2026-base`](https://github.com/macedot/rinha-2026-base).

> A ponte IVF C/AVX2 é compartilhada entre as implementações via submódulo git. Veja [`rinha-2026-base`](https://github.com/macedot/rinha-2026-base) para detalhes do algoritmo de busca.

## Início Rápido

```bash
docker compose up --build
```

A API escuta na porta `9999`.

## API

### `GET /ready`
Retorna `200 OK` quando a API está pronta.

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
|-----------|----------|------|
| **passa** | Rust | Balanceador round-robin sobre UDS |
| **fasthttp** | Go | Servidor HTTP, listener UDS |
| **Vetorizador** | Go | 14 dimensões seguindo normalização oficial |
| **Parser JSON** | Go | Parser customizado sem alocação |
| **Ponte IVF** | C/AVX2 (CGo) | Submódulo [`rinha-2026-base`](https://github.com/macedot/rinha-2026-base) |
| **build_index** | Go | Pré-processa `references.json.gz` em índice IVF1 |

## Configuração

| Variável | Padrão | Descrição |
|----------|---------|-----------|
| `IVF_NPROBE` | `8` | Clusters sondados na passada rápida |
| `IVF_FULL_NPROBE` | `24` | Clusters sondados na passada completa |
| `GOGC` | `100` | Percentual alvo do GC |
| `GOMEMLIMIT` | `100MiB` | Limite soft de memória |

## Estrutura

```
├── cmd/
│   ├── server/main.go       # Servidor HTTP fasthttp
│   └── build_index/main.go  # Construtor do índice IVF1
├── internal/
│   ├── ivfsearch/           # Ponte CGo → submódulo bridge/
│   ├── vectorizer/          # Vetorizador 14-dim
│   ├── jsonparse/           # Parser JSON zero-alocação
│   └── httpresp/            # Respostas HTTP pré-computadas
├── bridge/                  # Submódulo: macedot/rinha-2026-base
├── resources/
│   └── index.bin            # Índice IVF1 (3M vetores, 4096 clusters)
├── Dockerfile
├── docker-compose.yml
└── README.md
```

## CI/CD

GitHub Actions publica imagem `ghcr.io/macedot/rinha-2026-go` a cada release.

## Ambiente de Teste

Mac Mini Late 2014 (2.6 GHz Haswell, 8 GB RAM). Limites Docker: **1.0 CPU**, **350 MB** memória.

## Licença

MIT
