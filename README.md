# backend-war-go

Implementação em Go para a **Rinha de Backend 2026**, com foco em **baixa latência**, **previsibilidade** e **score de fraude via busca vetorial**.

## Sumário

1. [Problema da Rinha](#problema-da-rinha)
2. [Stack e escolhas](#stack-e-escolhas)
3. [Arquitetura (DDD)](#arquitetura-ddd)
4. [Fluxo completo (Mermaid)](#fluxo-completo-mermaid)
5. [Trade-offs técnicos](#trade-offs-técnicos)
6. [Regra de decisão](#regra-de-decisão)
7. [Endpoints](#endpoints)
8. [Configuração](#configuração)
9. [Como rodar](#como-rodar)
10. [Referências oficiais da Rinha](#referências-oficiais-da-rinha)

## Problema da Rinha

A API recebe transações em `POST /fraud-score` e devolve:

- `approved` (`true` ou `false`)
- `fraud_score` (`0.0` a `1.0`)

A decisão é baseada nos **5 vizinhos mais próximos** (top-5) no índice vetorial oficial.

## Stack e escolhas

- Go (stdlib para HTTP/JSON/sockets/concurrency)
- Docker + HAProxy (2 instâncias da API)
- Unix Domain Socket entre LB e APIs
- Índice vetorial `index.bin` (IVF)
- `mmap` para carga do índice (padrão de runtime)
- `sync.Pool` para reduzir alocações no hot path

## Arquitetura (DDD)

Separação por responsabilidade:

- `internal/domain/fraud`: entidades, validação e regra de negócio
- `internal/application/fraudscore`: use case (`Evaluate`) e contratos (ports)
- `internal/api`: camada HTTP + adapters para aplicação
- `internal/ivf`: leitura/parsing do índice vetorial
- `internal/api/search.go`: busca IVF otimizada no caminho de request

## Fluxo completo (Mermaid)

```mermaid
flowchart TB
    subgraph OFF[Offline: geração de índice]
        R[references.json.gz]
        M[mcc_risk.json]
        N[normalization.json]
        B[cmd/build-index]
        I[index.bin]
        R --> B
        M --> B
        N --> B
        B --> I
    end

    subgraph RUN[Runtime: infraestrutura]
        C[Cliente / Avaliador]
        LB[HAProxy :9999]
        A1[API 1]
        A2[API 2]
        C --> LB
        LB -->|UDS api1.sock| A1
        LB -->|UDS api2.sock| A2
        I -. mount ro .-> A1
        I -. mount ro .-> A2
    end

    subgraph REQ[Pipeline por request]
        H[POST /fraud-score]
        D[Decode JSON + limite 64KB]
        MP[Map DTO HTTP -> domínio]
        U[Use case Evaluate(ctx, Input)]
        V[Validação de domínio]
        F[Vectorize: 14 features]
        P[nprobe: escolhe centroides]
        S[Scan inicial]
        X{Voto parcial 2 ou 3?}
        R2[Refino global bbox LB]
        T[Top-5 final]
        FC[fraud_count 0..5]
        A{fraud_count >= 3?}
        AP[approved=false]
        OK[approved=true]
        SC[fraud_score = fraud_count / 5]
        OUT[JSON pre-serializado]

        H --> D --> MP --> U --> V --> F --> P --> S --> X
        X -- sim --> R2 --> T
        X -- não --> T
        T --> FC --> A
        A -- sim --> AP --> SC
        A -- não --> OK --> SC
        SC --> OUT
    end

    A1 --> H
    A2 --> H
```

## Trade-offs técnicos

### `NPROBE`

- **Escolha:** varredura inicial de poucos centroides, com refino quando o voto parcial fica ambíguo (`2` ou `3`).
- **Ganho:** reduz custo médio por request e melhora latência.
- **Custo:** `NPROBE` baixo pode perder recall em casos de fronteira.
- **Ajuste:** aumentar `NPROBE` tende a melhorar qualidade e piorar latência/CPU.

### `mmap` (`USE_MMAP=true`)

- **Escolha:** carregar `index.bin` com mapeamento de memória.
- **Ganho:** startup e acesso ao índice mais eficientes, sem cópias extras de leitura.
- **Custo:** depende mais do comportamento de paginação do host/contêiner.
- **Ajuste:** manter `true` no runtime alvo da Rinha.

### `GOMAXPROCS(1)`

- **Escolha:** fixar 1 P para previsibilidade sob orçamento estrito de CPU da competição.
- **Ganho:** reduz variação de latência e contenção de scheduler.
- **Custo:** limita paralelismo dentro do processo.
- **Ajuste:** em ambiente fora da Rinha, pode ser reavaliado conforme CPU disponível.

### `sync.Pool`

- **Escolha:** reutilizar buffers no decode do request e scratch em partes do hot path.
- **Ganho:** menos alocação e menor pressão no GC.
- **Custo:** complexidade maior no código e necessidade de disciplina de reset/reuso.
- **Ajuste:** manter só onde medido como benefício real de throughput/latência.

## Regra de decisão

- `fraud_count` é a contagem de fraudes no top-5
- `fraud_score = fraud_count / 5.0`
- `approved = fraud_count < 3`

## Endpoints

- `GET /ready`: health check
- `POST /fraud-score`: score antifraude

## Configuração

| Variável | Default | Descrição |
|---|---|---|
| `PORT` | `9999` | Porta TCP quando `UDS_PATH` não está definido |
| `UDS_PATH` | vazio | Caminho do socket Unix |
| `UDS_MODE` | `438` | Permissão decimal do socket (`0666`) |
| `USE_MMAP` | `true` | Usa mmap na carga do índice (recomendado manter `true`) |
| `INDEX_PATH` | `resources/index.bin` | Caminho do índice vetorial |
| `NPROBE` | `1` | Quantidade de centroides sondados no passo inicial |

## Como rodar

### Local

```bash
make test
make run
```

### Docker (stack de competição)

```bash
make docker-up
make docker-logs
make docker-down
```

### Gerar/atualizar índice

```bash
make data
make index
```

## Referências oficiais da Rinha

- Repositório oficial: `https://github.com/zanfranceschi/rinha-de-backend-2026`
- Documentação (PT-BR): `https://github.com/zanfranceschi/rinha-de-backend-2026/blob/main/docs/br/README.md`
- Contrato da API: `https://github.com/zanfranceschi/rinha-de-backend-2026/blob/main/docs/br/API.md`
- Regras de detecção: `https://github.com/zanfranceschi/rinha-de-backend-2026/blob/main/docs/br/REGRAS_DE_DETECCAO.md`
- Busca vetorial: `https://github.com/zanfranceschi/rinha-de-backend-2026/blob/main/docs/br/BUSCA_VETORIAL.md`
- Arquitetura e limites: `https://github.com/zanfranceschi/rinha-de-backend-2026/blob/main/docs/br/ARQUITETURA.md`
- Dataset e arquivos de referência: `https://github.com/zanfranceschi/rinha-de-backend-2026/blob/main/docs/br/DATASET.md`
