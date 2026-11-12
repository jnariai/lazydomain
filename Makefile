BIN     := bin/lazydomain
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install uninstall test test-install vet doctor

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/lazydomain

install: build
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/lazydomain

uninstall:
	rm -f $(PREFIX)/bin/lazydomain

test:
	go test ./...

test-install:
	tests/install_test.sh
	TEST_SH=bash tests/install_test.sh

vet:
	go vet ./...

doctor: build
	./$(BIN) doctor
