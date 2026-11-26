.PHONY: build-pc build-android test

BINARY_NAME := agente-bot

build-pc:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BINARY_NAME)-linux-amd64 ./cmd/bot

build-android:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o $(BINARY_NAME)-android-arm64 ./cmd/bot

test:
	go test ./...
