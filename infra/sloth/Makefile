# Sloth SLO spec management.
# Requires: go install github.com/slok/sloth/cmd/sloth@latest

OUTPUT := ../helm/monitoring/manifests/prometheusrule-slo-generated.yaml

.PHONY: generate validate

generate:
	sloth generate -i *.yaml -o $(OUTPUT)
	@echo "SLO rules generated at $(OUTPUT)"

validate:
	@for f in *.yaml; do \
		echo "Validating $$f..."; \
		sloth validate -i "$$f"; \
	done
	@echo "All SLO specs valid"
