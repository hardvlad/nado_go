BINARY  := bin/nado.exe
PKG     := ./cmd/nado
JOBS_PKG := ./cmd/nado-jobs
ADMIN_PKG := ./cmd/nado-admin
VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: run build build-jobs build-admin build-linux test vet fmt tidy migrate clean

## run — запустить приложение локально
run:
	go run -ldflags "$(LDFLAGS)" $(PKG)

## build — собрать бинарник с зашитыми шаблонами и статикой
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

## build-jobs — собрать бинарник удалённого воркера
build-jobs:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/nado-jobs.exe $(JOBS_PKG)

## build-admin — собрать админ-бинарник
build-admin:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/nado-admin.exe $(ADMIN_PKG)

## build-linux — статические бинарники сервера и воркера для Linux (как в CI)
build-linux: export GOOS=linux
build-linux: export GOARCH=amd64
build-linux: export CGO_ENABLED=0
build-linux:
	go build -trimpath -ldflags "-s -w $(LDFLAGS)" -o bin/nado $(PKG)
	go build -trimpath -ldflags "-s -w $(LDFLAGS)" -o bin/nado-jobs $(JOBS_PKG)
	go build -trimpath -ldflags "-s -w $(LDFLAGS)" -o bin/nado-admin $(ADMIN_PKG)

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
	sqlcmd -S $(DB_HOST) -d $(DB_NAME) -U $(DB_USER) -P $(DB_PASSWORD) -C -i migrations/0001_init.sql
	sqlcmd -S $(DB_HOST) -d $(DB_NAME) -U $(DB_USER) -P $(DB_PASSWORD) -C -i migrations/0002_accounts_auth_feedback.sql
	sqlcmd -S $(DB_HOST) -d $(DB_NAME) -U $(DB_USER) -P $(DB_PASSWORD) -C -i migrations/0003_jobs.sql
	sqlcmd -S $(DB_HOST) -d $(DB_NAME) -U $(DB_USER) -P $(DB_PASSWORD) -C -i migrations/0004_messaging_otp.sql
	sqlcmd -S $(DB_HOST) -d $(DB_NAME) -U $(DB_USER) -P $(DB_PASSWORD) -C -i migrations/0005_stores_marketplace.sql
	sqlcmd -S $(DB_HOST) -d $(DB_NAME) -U $(DB_USER) -P $(DB_PASSWORD) -C -i migrations/0006_marketplace_products.sql
	sqlcmd -S $(DB_HOST) -d $(DB_NAME) -U $(DB_USER) -P $(DB_PASSWORD) -C -i migrations/0007_kaspi_mfa_codes.sql
	sqlcmd -S $(DB_HOST) -d $(DB_NAME) -U $(DB_USER) -P $(DB_PASSWORD) -C -i migrations/0008_kaspi_onboarding.sql

clean:
	rm -rf bin
