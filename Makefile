FRONTEND_DIR := src/frontend
SERVER_DIR := src

ifeq ($(OS),Windows_NT)
	BINARY := caw.exe
else
	BINARY := caw
endif

.PHONY: build lint

build:
	cd $(FRONTEND_DIR) && npm install && npm run build
	cd $(SERVER_DIR) && go build -o ../$(BINARY) ./cmd/caw/

lint:
	cd $(FRONTEND_DIR) && npm run lint
	cd $(SERVER_DIR) && go vet ./...
