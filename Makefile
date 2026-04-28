.PHONY: build test lint clean

build:
	go build -o bin/meridian ./cmd/meridian

test:
	go test ./... -v -race

lint:
	golangci-lint run

clean:
	rm -rf bin/
