$(eval RELEASE_TAG := $(shell jq -r '.release_tag' component.json))
$(eval VERSION := $(shell jq -r '.version' component.json))
$(eval REGISTRY := $(shell jq -r '.registry' component.json))

.PHONY: all build release test clean
GOVERSION ?= go
build:
	$(GOVERSION) build -v ./...
release:
	git tag -a $(RELEASE_TAG) -m "Release $(RELEASE_TAG)"
	git push origin $(RELEASE_TAG)
rm-release:
	git tag -d $(RELEASE_TAG)
	git push origin :refs/tags/$(RELEASE_TAG)
test:
	CGO_ENABLED=1 $(GOVERSION) test -v -race -coverprofile=coverage.txt -covermode=atomic ./...
test/bench:
	$(GOVERSION) mod tidy && $(GOVERSION) test -bench=. -benchmem ./pkg/...
test/bench-integration:
	$(GOVERSION) mod tidy && $(GOVERSION) test -bench=. -benchmem --tags=integration ./pkg/...
test/integration:
	$(GOVERSION) test -v --tags=integration ./...
clean:
	rm -rf $(CURDIR)/bin
