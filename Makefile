.PHONY: all build test lint run docker-build clean

all: build test

build:
	go build -v -trimpath ./cmd/reprox

test:
	go test -v -race ./...

lint:
	go vet ./...
	gofmt -l internal cmd

run: build
	./reprox

docker-build:
	docker build -t reprox:latest .

clean:
	rm -f reprox
