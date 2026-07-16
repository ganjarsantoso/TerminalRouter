.PHONY: build test race fmt vet clean

VERSION ?= 0.1.0-dev

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/termrouter ./cmd/termrouter

test:
	go test ./...

race:
	go test -race ./...

fmt:
	gofmt -w $(shell find . -name '*.go' -not -path './prd/*')

vet:
	go vet ./...

clean:
	rm -rf bin/
