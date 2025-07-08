VERSION ?= 0.0.3

build:
	@echo "building gocue..."
	go build -o ./dist/gocue -ldflags="-X 'github.com/iSerganov/gocue/cmd/cue.version=${VERSION}'" main.go 
	@echo "building of gocue completed."
.PHONY: build
