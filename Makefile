BINARY=cs
VERSION?=dev
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

.PHONY: build test lint tidy verify clean

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/cs

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum not tidy" && exit 1)

vet:
	go vet ./...

verify: build vet lint test tidy
	@echo "All checks passed"

clean:
	rm -rf bin/

install: build
	cp bin/$(BINARY) $(GOPATH)/bin/$(BINARY)
