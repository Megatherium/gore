.PHONY: all build test lint run docker-build clean

all: build test

build:
	go build -v -trimpath ./cmd/gore

test:
	go test -v -race ./...

lint:
	go vet ./...
	gofmt -l internal cmd

run: build
	./gore

docker-build:
	docker build -t gore:latest .

clean:
	rm -f gore
