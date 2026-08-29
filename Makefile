.PHONY: build install test vet fmt tidy clean run all

BINARY := hiroto
INSTALL_PATH := $(HOME)/.local/bin/$(BINARY)

build:
	go build -o $(BINARY) ./cmd/hiroto

install: build
	mkdir -p $(dir $(INSTALL_PATH))
	cp $(BINARY) $(INSTALL_PATH)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)

run: build
	./$(BINARY)

all: fmt vet test build
