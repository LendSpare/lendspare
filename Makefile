# Common development tasks. Needs Go 1.25+; `lint` and `fmt` need golangci-lint v2.

GO     ?= go
DOCKER ?= docker
BIN    := bin/lendspare$(shell $(GO) env GOEXE)

.PHONY: all build run test race cover vet fmt fmt-check lint tidy dist dist-signed msix docker clean

all: fmt-check vet lint test build

build:
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/lendspare

run:
	$(GO) run ./cmd/lendspare $(ARGS)

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -n 1

vet:
	$(GO) vet ./...

fmt:
	golangci-lint fmt

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

lint:
	golangci-lint run

tidy:
	$(GO) mod tidy

# Release binaries for Linux, Windows and macOS with checksums (see scripts/build.sh).
dist:
	bash scripts/build.sh

# The same, with the Windows exe signed (Azure Artifact Signing, on Windows) before it is hashed.
dist-signed:
	SIGN_CMD="bash $(CURDIR)/scripts/sign-windows.sh" bash scripts/build.sh

# Microsoft Store package (on Windows; the Store signs it). See scripts/pack-msix.sh.
msix:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-H=windowsgui" -o dist/lendspare-windows.exe ./cmd/lendspare
	bash scripts/pack-msix.sh dist/lendspare-windows.exe

docker:
	$(DOCKER) build -t lendspare:local .

clean:
	rm -rf bin build dist coverage.out
