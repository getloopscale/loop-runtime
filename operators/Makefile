.PHONY: test lint fmt vet tidy manifests generate controller-gen test-short

# Tool versions
CONTROLLER_TOOLS_VERSION ?= v0.16.5

# Tool binaries
LOCALBIN ?= $(shell pwd)/bin
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen

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

## manifests: Generate CRD manifests (YAML) from kubebuilder markers
manifests: controller-gen
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd/bases

## generate: Generate DeepCopy methods from kubebuilder markers
generate: controller-gen
	$(CONTROLLER_GEN) object paths="./api/..."

## controller-gen: Download controller-gen locally if necessary
controller-gen: $(CONTROLLER_GEN)
$(CONTROLLER_GEN): $(LOCALBIN)
	@test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

$(LOCALBIN):
	mkdir -p $(LOCALBIN)
