$(eval RELEASE_TAG := $(shell jq -r '.release_tag' component.json))
$(eval VERSION := $(shell jq -r '.version' component.json))
$(eval REGISTRY := $(shell jq -r '.registry' component.json))

.PHONY: all build release test clean

build:
	go build
release:
	git tag -a $(RELEASE_TAG) -m "Release $(RELEASE_TAG)"
	git push origin $(RELEASE_TAG)
rm-release:
	git tag -d $(RELEASE_TAG)
	git push origin :refs/tags/$(RELEASE_TAG)
test:
	go test -v -cover ./...
test/bench:
	go mod tidy && go test -bench=. -benchmem ./pkg/...
test/integration:
	go test -v --tags=integration ./...
clean:
	rm -rf $(CURDIR)/bin
