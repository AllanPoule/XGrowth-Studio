VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w

.PHONY: build test release clean

build:
	go build -ldflags "$(LDFLAGS)" -o xgrowth .

test:
	go vet ./...
	go test ./...
	@if command -v node >/dev/null 2>&1; then node web/tz.test.js; else echo "node absent : tests JS (fuseaux) ignorés"; fi

release:
	mkdir -p dist
	GOOS=linux  GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/xgrowth-linux-amd64 .
	GOOS=linux  GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/xgrowth-linux-arm64 .
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/xgrowth-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/xgrowth-darwin-amd64 .

clean:
	go clean
	rm -f xgrowth
	rm -rf dist
