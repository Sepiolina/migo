GO ?= go
GOFMT ?= gofmt
CONFIG ?= config.json

.DEFAULT_GOAL := help
.PHONY: help build build-all linux windows test vet race fmt fmt-check workflows check run

help:
	@echo 'make build       Build the service and CLI'
	@echo 'make run         Run the service (CONFIG=config.json)'
	@echo 'make test        Run vet and unit tests'
	@echo 'make check       Run the same quality checks as CI (requires a C compiler)'
	@echo 'make fmt         Format Go source files'
	@echo 'make workflows   Validate the three example workflows without company services'
	@echo 'make linux/windows  Cross-build a static service binary into dist/'

build:
	$(GO) build -o miservice ./cmd/miservice
	$(GO) build -o migo ./cmd/migo
build-all:
	$(GO) build ./...
linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags="-s -w" -o dist/miservice ./cmd/miservice
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -ldflags="-s -w" -o dist/miservice.exe ./cmd/miservice
run:
	$(GO) run ./cmd/miservice -config "$(CONFIG)"
vet:
	$(GO) vet ./...
test: vet
	$(GO) test ./...
race:
	$(GO) test -race ./...
fmt:
	$(GOFMT) -w cmd internal workflows
fmt-check:
	@files="$$($(GOFMT) -l cmd internal workflows)" || exit 1; \
	if [ -n "$$files" ]; then echo "Run make fmt to format:"; echo "$$files"; exit 1; fi
workflows:
	$(GO) run ./cmd/migo -check -in bom=testdata/bom_basic.xlsx -in priority=testdata/prio_basic.xlsx workflows/bom.yaml
	$(GO) run ./cmd/migo -check -cfg '{"db_enabled": true}' -sql-stub testdata/stub_master.json -in bom=testdata/bom_random7.xls -in priority=testdata/prio_random7.xls workflows/bom.yaml
	$(GO) run ./cmd/migo -check -in parts=examples/parts.xlsx examples/parts-summary.yaml
check: fmt-check vet build-all race workflows
