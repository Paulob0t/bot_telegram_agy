.PHONY: build-pc build-android test install

BINARY_NAME := agente-bot

build-pc:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BINARY_NAME)-linux-amd64 ./cmd/bot

build-android:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o $(BINARY_NAME)-android-arm64 ./cmd/bot

test:
	go test ./...

# Installs the binary the Claude/AGY hooks call and links the .env they read.
install:
	go build -o $(HOME)/.local/bin/$(BINARY_NAME) ./cmd/bot
	go build -o $(CURDIR)/$(BINARY_NAME) ./cmd/bot
	mkdir -p $(HOME)/.config/agente-bot
	ln -sf $(CURDIR)/.env $(HOME)/.config/agente-bot/.env
