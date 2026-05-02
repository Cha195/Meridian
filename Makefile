.PHONY: build test lint clean run

build:
	go build -o bin/meridian ./cmd/meridian

test:
	go test ./... -v -race

lint:
	golangci-lint run

clean:
	rm -rf bin/

run:
	go run ./cmd/meridian serve --config config.example.yaml
