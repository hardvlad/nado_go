BINARY  := bin/nado.exe
PKG     := ./cmd/nado
VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: run build test vet fmt tidy migrate clean

## run — запустить приложение локально
run:
	go run -ldflags "$(LDFLAGS)" $(PKG)

## build — собрать бинарник с зашитыми шаблонами и статикой
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

## test — прогнать тесты с гонками
test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

## migrate — применить миграции (нужен sqlcmd)
migrate:
	sqlcmd -S $(DB_HOST) -d $(DB_NAME) -U $(DB_USER) -P $(DB_PASSWORD) -i migrations/0001_init.sql

clean:
	rm -rf bin
