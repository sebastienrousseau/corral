BINARY_NAME=corralctl

.PHONY: all build test test-race vet lint clean format

all: format vet test test-race build

build:
	go build -o $(BINARY_NAME) main.go

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

format:
	go fmt ./...

clean:
	go clean
	rm -f $(BINARY_NAME)
