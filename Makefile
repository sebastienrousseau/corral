BINARY_NAME=corral

.PHONY: all build test clean format

all: format test build

build:
	go build -o $(BINARY_NAME) main.go

test:
	go test ./...

format:
	go fmt ./...

clean:
	go clean
	rm -f $(BINARY_NAME)
