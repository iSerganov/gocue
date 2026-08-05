VERSION ?= 1.1.2

build:
	@echo "building gocue..."
	go build -o ./dist/gocue -ldflags="-X 'github.com/iSerganov/gocue/cmd/cue.version=${VERSION}'" main.go
	@echo "building of gocue completed."

test:
	go test -race -count=1 ./...

test-integration:
	go test -tags=integration ./integration/ -count=1 -timeout 20m

.PHONY: build test test-integration
