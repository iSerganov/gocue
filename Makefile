VERSION ?= 1.3.0
FFMPEG_PREFIX ?= $(CURDIR)/.ffmpeg

# Once `make ffmpeg` has built the static FFmpeg, every Go command links
# against it. CGO_* flags from the environment are cleared so they cannot
# point the link back at a shared FFmpeg.
FFMPEG_ENV = $(if $(wildcard $(FFMPEG_PREFIX)/lib/pkgconfig),PKG_CONFIG_PATH=$(FFMPEG_PREFIX)/lib/pkgconfig CGO_CFLAGS= CGO_CPPFLAGS= CGO_LDFLAGS=)

ffmpeg:
	scripts/build-ffmpeg.sh $(FFMPEG_PREFIX)

build:
	@echo "building gocue..."
	$(FFMPEG_ENV) go build -o ./dist/gocue -ldflags="-X 'github.com/iSerganov/gocue/cmd/cue.version=${VERSION}'" main.go
	@echo "building of gocue completed."

test:
	$(FFMPEG_ENV) go test -race -count=1 ./...

test-integration:
	$(FFMPEG_ENV) go test -tags=integration ./integration/ -count=1 -timeout 20m

bench:
	$(FFMPEG_ENV) go test ./pkg/cue -run '^$$' -bench '^Benchmark(Calc|Probe|Scan)$$' -benchmem -benchtime 3x -count 10 -timeout 60m

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

.PHONY: ffmpeg build test test-integration bench example-env example-playlist example-up example-down example-logs
