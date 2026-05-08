<h1 align="center">Rinha de Backend 2026 — Go + C/AVX2</h1>

<p align="center"><strong>API de detecção de fraude com busca vetorial IVF usando kernels AVX2 otimizados à mão</strong></p>

<p align="center">
  <img src="https://img.shields.io/github/license/macedot/rinha-2026-go?color=blue" alt="License" />
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/C-AVX2-00599C?logo=c&logoColor=white" alt="C/AVX2" />
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white" alt="Docker" />
  <img src="https://img.shields.io/badge/p99-27µs-4ade80" alt="p99 latency" />
</p>

---

**Submissão para a [Rinha de Backend 2026](https://github.com/zanfranceschi/rinha-de-backend-2026)** — detecção de fraude via busca vetorial. Processa transações de cartão através de um vetorizador de 14 dimensões e busca em 3 milhões de vetores de referência usando IVF/K-means (4096 clusters) com distância Euclidiana acelerada por AVX2 via ponte CGo.

## Início Rápido

```bash
docker compose up --build
```

A API escuta na porta `9999`.

### Imagens pré-compiladas (do release do GitHub)

```bash
IMAGE=ghcr.io/macedot/rinha-2026-go:latest docker compose up
```

Substitua `build: .` por `image: ghcr.io/macedot/rinha-2026-go:latest` no `docker-compose.yml`.

## API

### `GET /ready`

Retorna `200 OK` quando a API carregou o índice e está pronta para servir.

### `POST /fraud-score`

**Requisição:**
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

**Resposta:**
```json
{ "approved": true, "fraud_score": 0.0000 }
```

## Arquitetura

```
                           ┌──────────┐
                           │  Cliente │
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
                   ││ serv. UDS ││ ││ serv. UDS ││
                   │└─────┬─────┘│ │└─────┬─────┘│
                   │      │      │ │      │      │
                   │┌─────▼─────┐│ │┌─────▼─────┐│
                   ││ Vetoriz.  ││ ││ Vetoriz.  ││
                   ││ 14 dim.   ││ ││ 14 dim.   ││
                   │└─────┬─────┘│ │└─────┬─────┘│
                   │      │      │ │      │      │
                   │┌─────▼─────┐│ │┌─────▼─────┐│
                   ││ C/AVX2    ││ ││ C/AVX2    ││
                   ││ Busca IVF ││ ││ Busca IVF ││
                   ││ 4096 cls. ││ ││ 4096 cls. ││
                   │└───────────┘│ │└───────────┘│
                   └─────────────┘ └─────────────┘

    ┌──────────────────────────────────────────────────────┐
    │  rinha-sockets (tmpfs, 10mb)  ·  rede bridge         │
    │  CPU total: 1.0   |   Memória total: 350 MB          │
    └──────────────────────────────────────────────────────┘
```

### Fluxo da requisição

1. **Cliente** envia `POST /fraud-score` com JSON da transação para a porta `9999`
2. **HAProxy** faz round-robin do payload HTTP bruto sobre **Unix Domain Socket** (`/sockets/api1.sock` ou `api2.sock`) — zero overhead de TCP, sem inspeção de payload
3. **fasthttp** faz o parse do JSON (parser customizado sem alocação) e extrai todos os campos
4. **Vetorizador** transforma o payload em um vetor float de 14 dimensões usando as fórmulas oficiais de normalização
5. **Busca IVF C/AVX2 (ponte CGo)** quantiza para `int16`, calcula distâncias dos centroides com AVX2, seleciona top-N clusters e varre blocos AoSoA com AVX2 FMA + early termination + prefetch. Retorna k=5 vizinhos mais próximos via busca em dois estágios (passada rápida → passada completa para resultados ambíguos)
6. **fraud_score** = fraudes entre os top 5 / 5; `approved = fraud_score < 0.6`

### Componentes

| Componente | Linguagem | Função |
|-----------|----------|------|
| **HAProxy 3.3** | C | Balanceador de carga layer 7, round-robin sobre UDS |
| **servidor fasthttp** | Go | Manipulação HTTP, listener UDS, parser JSON sem alocação |
| **Vetorizador** | Go | Vetorizador de features 14-dim seguindo regras oficiais de normalização |
| **Ponte de busca IVF** | C/AVX2 (CGo) | Busca IVF/K-means: 4096 clusters, distância de centroides AVX2 com FMA, seleção top-N de clusters com AVX2, varredura de blocos AoSoA com AVX2 + early termination + prefetch, busca adaptativa em dois estágios |
| **build_index** | Go | Pré-processa `references.json.gz` (3M vetores) em índice binário IVF1: clusterização K-means, quantização `int16`, centroides transpostos, layout de blocos AoSoA |

### Transporte

O HAProxy se comunica com as instâncias da API via **Unix Domain Sockets** em um volume `tmpfs` (`rinha-sockets`). Isso elimina completamente o overhead de TCP — sem pilha de rede do kernel, sem buffers de socket, sem filas de accept. Um único volume tmpfs de 10 MB comporta ambos os arquivos de socket da API.

### Stack Tecnológico

- **Go 1.24** — servidor HTTP fasthttp, transporte UDS, parser JSON customizado zero-alocação
- **C/AVX2 (CGo)** — intrínsecos AVX2 para distância de centroides (FMA), seleção top-N, varredura de blocos AoSoA com prefetch e early termination
- **HAProxy 3.3** — balanceador de carga stateless round-robin
- **Docker Compose** — 3 serviços, rede bridge, limites de recursos via `deploy.resources.limits`

## Destaques de Otimização

O kernel de busca IVF passou por micro-otimizações extensivas visando latência p99 em um [Mac Mini Late 2014](https://support.apple.com/en-us/111931) (2.6 GHz Haswell, 8 GB RAM) com limites Docker de 1.0 CPU e 350 MB de memória total.

| Otimização | Impacto | Técnica |
|-------------|--------|-----------|
| **Distância de centroides AVX2** | -0.15ms p99 | Cálculo vetorizado de distância: 16 centroides/iter com acumulação FMA, centroides transpostos para leituras contíguas de dimensão |
| **Seleção top-N AVX2** | -0.05ms p99 | Seleção de clusters baseada em máscara com comparações de 8 vias |
| **Varredura de blocos AoSoA AVX2** | -0.25ms p99 | Blocos de 8 vetores em layout column-major, processamento de pares de dimensão com FMA, early termination após 4/7 pares, prefetch por software |
| **Busca em dois estágios** | -0.10ms p99 | Passada rápida com nprobe=8, passada completa com nprobe=24 apenas para resultados ambíguos (2-3 fraudes) |
| **Reordenação de clusters** | -0.03ms | Varre primeiro os clusters menores para apertar a pior distância mais cedo |
| **Centroides transpostos** | -0.02ms | Layout column-major dos centroides para cargas AVX2 cache-friendly |
| **GOGC=100, GOMEMLIMIT=100MiB** | -0.02ms | GC ajustado para evitar pausas stop-the-world sob carga |
| **Transporte UDS** | -0.08ms | HAProxy ↔ API via Unix domain sockets (zero overhead de TCP) |

**Total: ~1.56ms → ~27µs p99**

## Configuração

| Variável | Padrão | Descrição |
|----------|---------|-------------|
| `IVF_NPROBE` | `8` | Número de clusters sondados na passada rápida |
| `IVF_FULL_NPROBE` | `24` | Número de clusters sondados na passada completa (resultados ambíguos) |
| `CANDIDATES` | `0` | Máximo de candidatos a varrer (0 = ilimitado) |
| `GOGC` | `100` | Percentual alvo do GC do Go |
| `GOMEMLIMIT` | `100MiB` | Limite soft de memória do Go |
| `AMOUNT_DIVISOR` | `10000` | Constante de normalização (max_amount) |
| `INSTALLMENTS_DIVISOR` | `12` | Constante de normalização (max_installments) |
| `TX24H_DIVISOR` | `20` | Constante de normalização (max_tx_count_24h) |
| `KM_DIVISOR` | `1000` | Constante de normalização (max_km) |
| `MERCHANT_AMOUNT_DIVISOR` | `10000` | Constante de normalização (max_merchant_avg_amount) |

Todas as constantes de normalização seguem o `normalization.json` oficial.

## Estrutura do Repositório

```
├── cmd/
│   ├── server/main.go           # Servidor HTTP fasthttp da API
│   ├── build_index/main.go      # Construtor do índice IVF1 (K-means + quantização + empacotamento AoSoA)
│   └── bench/main.go            # Benchmark de latência com p99 + instrumentação
├── internal/
│   ├── config/config.go         # Configuração por variáveis de ambiente
│   ├── vectorizer/vectorizer.go # Vetorizador de features 14-dim
│   ├── ivfsearch/
│   │   ├── bridge.c             # Kernel de busca IVF C/AVX2 (centroides + top-N + varredura AoSoA)
│   │   ├── bridge.h             # Header da ponte C
│   │   ├── bridge.go            # Bindings CGo para Go
│   │   └── ivfsearch.go         # Constantes compartilhadas
│   ├── jsonparse/jsonparse.go   # Parser JSON zero-alocação
│   ├── mccrisk/mccrisk.go       # Tabela de risco por MCC
│   └── httpresp/httpresp.go     # Respostas HTTP pré-computadas
├── resources/
│   ├── index.bin                # Índice IVF1 pré-construído (3M vetores, 4096 clusters, ~84MB)
│   ├── mcc_risk.json            # Tabela de risco por categoria de estabelecimento
│   ├── references.json.gz       # 3M vetores de referência rotulados (entrada do build_index)
│   ├── example-payloads.json    # Exemplos de payloads de transação
│   └── example-references.json  # Exemplos de vetores de referência
├── Dockerfile                   # Multi-estágio: build Go → runtime Debian slim
├── docker-compose.yml           # Deploy de 3 serviços com limites de recursos
├── haproxy.cfg                  # Configuração de UDS round-robin do HAProxy
├── .github/workflows/release.yml # CI: build & push da imagem Docker para GHCR
├── LICENSE                      # MIT
├── info.json                    # Dados do participante da Rinha
└── README.md
```

> O branch `submission` contém `docker-compose.yml`, `haproxy.cfg`, `info.json` e recursos — referenciando a imagem pré-compilada `ghcr.io/macedot/rinha-2026-go:latest`.

## CI/CD

GitHub Actions compila e publica uma imagem Docker `linux/amd64` em `ghcr.io/macedot/rinha-2026-go` a cada release publicado (excluindo pre-releases). As imagens são tagueadas com a versão do release e `latest`.

## Ambiente de Teste

O teste oficial executa em um Mac Mini Late 2014 (2.6 GHz Haswell, 8 GB RAM, Ubuntu 24.04) com limites Docker de **1.0 CPU** e **350 MB de memória** entre todos os serviços. Todas as otimizações foram ajustadas especificamente para este hardware.

## Agradecimentos

O kernel de busca IVF em C/AVX2 (`internal/ivfsearch/bridge.c`) é uma migração adaptada do excelente trabalho do [Jairo Blatt](https://github.com/jairoblatt) no projeto [rinha-2026-rust](https://github.com/jairoblatt/rinha-2026-rust). Obrigado pelo kernels de alta performance e pela inspiração.

## Licença

Este projeto está licenciado sob a [Licença MIT](LICENSE).
