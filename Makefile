.PHONY: help data index build build-index run test fmt docker-up docker-down docker-logs docker-ps docker-build docker-push docker-publish clean

DEFAULT_GOAL := help

GO ?= go
COMPOSE ?= docker compose
DOCKER ?= docker

GHCR_USER ?= nicolasmmb
IMAGE_NAME ?= backend-war-go
IMAGE_TAG ?= latest
IMAGE ?= ghcr.io/$(GHCR_USER)/$(IMAGE_NAME)

PORT ?= 9999
INDEX_PATH ?= ./resources/index.bin
USE_MMAP ?= true
NPROBE ?= 1
UDS_PATH ?=
UDS_MODE ?= 438

IVF_K ?= 512
IVF_ITERS ?= 10
IVF_SEED ?= 16045690984503098046

REFS := resources/references.json.gz
INDEX := resources/index.bin
MCC := resources/mcc_risk.json
NORM := resources/normalization.json
GH_RAW := https://github.com/zanfranceschi/rinha-de-backend-2026/raw/main/resources

help:
	@echo "Targets:"
	@echo "  make data         - baixa references.json.gz/mcc_risk.json/normalization.json"
	@echo "  make index        - gera resources/index.bin"
	@echo "  make build        - builda API"
	@echo "  make build-index  - builda comando build-index"
	@echo "  make run          - roda API local"
	@echo "  make test         - roda testes"
	@echo "  make fmt          - formata codigo"
	@echo "  make docker-up    - sobe stack Docker"
	@echo "  make docker-down  - derruba stack Docker"
	@echo "  make docker-logs  - logs da stack"
	@echo "  make docker-ps    - status da stack"
	@echo "  make docker-build - builda imagem $(IMAGE):$(IMAGE_TAG)"
	@echo "  make docker-push  - publica imagem $(IMAGE):$(IMAGE_TAG)"
	@echo "  make docker-publish - builda e publica imagem $(IMAGE):$(IMAGE_TAG)"
	@echo "  make clean        - remove binarios locais"

resources:
	@mkdir -p resources

data: resources $(REFS) $(MCC) $(NORM)

$(REFS): resources
	curl -sSfL $(GH_RAW)/references.json.gz -o $@

$(MCC): resources
	curl -sSfL $(GH_RAW)/mcc_risk.json -o $@

$(NORM): resources
	curl -sSfL $(GH_RAW)/normalization.json -o $@

index: $(INDEX)

$(INDEX): $(REFS)
	IVF_K=$(IVF_K) IVF_ITERS=$(IVF_ITERS) IVF_SEED=$(IVF_SEED) $(GO) run ./cmd/build-index $(REFS) $(INDEX)

build:
	$(GO) build -o api .

build-index:
	$(GO) build -o build-index ./cmd/build-index

run:
	PORT=$(PORT) INDEX_PATH=$(INDEX_PATH) USE_MMAP=$(USE_MMAP) NPROBE=$(NPROBE) UDS_PATH=$(UDS_PATH) UDS_MODE=$(UDS_MODE) $(GO) run .

test:
	$(GO) test ./...

fmt:
	gofmt -w $(shell rg --files -g '*.go')

docker-up:
	$(COMPOSE) up --build -d --remove-orphans --force-recreate

docker-down:
	$(COMPOSE) down -v

docker-logs:
	$(COMPOSE) logs -f --tail=200

docker-ps:
	$(COMPOSE) ps

docker-build:
	$(DOCKER) build -t $(IMAGE):$(IMAGE_TAG) .

docker-push:
	$(DOCKER) push $(IMAGE):$(IMAGE_TAG)

docker-publish: docker-build docker-push

clean:
	rm -f api build-index
