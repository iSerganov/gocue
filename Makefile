VERSION ?= 1.2.0

build:
	@echo "building gocue..."
	go build -o ./dist/gocue -ldflags="-X 'github.com/iSerganov/gocue/cmd/cue.version=${VERSION}'" main.go
	@echo "building of gocue completed."

test:
	go test -race -count=1 ./...

test-integration:
	go test -tags=integration ./integration/ -count=1 -timeout 20m

EXAMPLE_COMPOSE = docker compose -f example/docker-compose.yml

example-env:
	@test -f example/.env || { \
		echo "example/.env is missing. Run:"; \
		echo "  cp example/.env.example example/.env"; \
		echo "then set MUSIC_DIR to the folder holding your audio files."; \
		exit 1; \
	}

example-playlist: example-env
	@./example/scripts/make-playlist.sh

# Launch the test station: builds gocue from this tree, starts Icecast and
# Liquidsoap, streams Ogg Vorbis.
example-up: example-playlist
	$(EXAMPLE_COMPOSE) up --build -d
	@port=$$(sed -n 's/^[[:space:]]*ICECAST_PUBLIC_PORT=//p' example/.env | tail -n 1); \
	harbor=$$(sed -n 's/^[[:space:]]*STATION_HARBOR_PORT=//p' example/.env | tail -n 1); \
	echo ""; \
	echo "stream    http://localhost:$${port:-8000}/radio.ogg"; \
	echo "icecast   http://localhost:$${port:-8000}/"; \
	echo "metadata  http://localhost:$${harbor:-8080}/metadata"; \
	echo "logs      make example-logs"

example-down:
	$(EXAMPLE_COMPOSE) down

example-logs:
	$(EXAMPLE_COMPOSE) logs -f station

.PHONY: build test test-integration example-env example-playlist example-up example-down example-logs
