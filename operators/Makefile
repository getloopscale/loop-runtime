.PHONY: test lint fmt vet tidy

## test: Run all unit tests
test:
	go test ./... -v -count=1

## test-short: Run all unit tests without verbose output
test-short:
	go test ./... -count=1

## lint: Run go vet
lint: vet

## vet: Run go vet on all packages
vet:
	go vet ./...

## fmt: Format all Go files
fmt:
	gofmt -w .

## tidy: Run go mod tidy
tidy:
	go mod tidy
