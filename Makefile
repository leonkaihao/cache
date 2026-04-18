outdir ?= $(CURDIR)/bin

build:
	go build
test:
	go test -v -cover ./...
test/integration:
	go test -v --tags=integration ./...
test/bench:
	go test -bench=. -benchmem
test/pack:
	go test -c -o=$(outdir)/platform.common.cache.test
clean:
	rm -rf $(CURDIR)/bin
