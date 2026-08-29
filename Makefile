.PHONY: build install test vet clean run

BINARY=hiroto
INSTALL_PATH=$(HOME)/.local/bin/$(BINARY)

build:
	go build -o $(BINARY) ./cmd/hiroto

install: build
	cp $(BINARY) $(INSTALL_PATH)

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)

run: build
	./$(BINARY)

fmt:
	gofmt -w .

tidy:
	go mod tidy

all: vet test build install