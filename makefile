BINARY := materialmaker-mcp
CMD := ./cmd/materialmaker-mcp
BIN_DIR := ./bin
TEST_PACKAGES ?= ./internal/... ./cmd/... ./tests/...
TEST_ARGS ?=

ifeq ($(OS),Windows_NT)
SHELL := powershell.exe
.SHELLFLAGS := -NoProfile -NonInteractive -Command
BINARY := materialmaker-mcp.exe
endif

.PHONY: build format format-check vet lint test test-race check clean

build:
ifeq ($(OS),Windows_NT)
	New-Item -ItemType Directory -Force -Path '$(BIN_DIR)' | Out-Null
else
	mkdir -p $(BIN_DIR)
endif
	go build -o "$(BIN_DIR)/$(BINARY)" $(CMD)

format:
	go fmt ./...

format-check:
ifeq ($(OS),Windows_NT)
	@$$files = gofmt -l .; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE }; if ($$files) { Write-Output 'Files need formatting:'; $$files; exit 1 }
else
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)
endif

vet:
	go vet ./...

lint:
	golangci-lint run ./...

test:
	go test -timeout=2m -count=1 $(TEST_ARGS) $(TEST_PACKAGES)

test-race:
	go test -race -timeout=3m -count=1 $(TEST_ARGS) $(TEST_PACKAGES)

check: format-check vet lint

clean:
ifeq ($(OS),Windows_NT)
	if (Test-Path -LiteralPath '$(BIN_DIR)') { Remove-Item '$(BIN_DIR)' -Recurse -Force }
else
	rm -rf $(BIN_DIR)
endif
