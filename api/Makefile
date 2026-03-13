.PHONY: test test-short lint vet fmt tidy build

test:
	go test ./... -v -count=1

test-short:
	go test ./... -short

lint: vet
	@echo "Lint passed (go vet)"

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

build:
	go build ./...
