# Builds gocue from this repository and drops it into the official Liquidsoap
# image next to the production autocue provider.
#
# The build context is the repository root, not example/:
#   docker build -f example/Dockerfile -t gocue-station ..

FROM golang:1.27 AS build

# gocue links a static FFmpeg 9.0.1, the version the tests run against, built
# by the same script as CI. Liquidsoap's image keeps its own FFmpeg 7.1.
RUN apt-get update \
 && apt-get install -y --no-install-recommends nasm xz-utils \
 && rm -rf /var/lib/apt/lists/*
COPY scripts/build-ffmpeg.sh /tmp/build-ffmpeg.sh
RUN /tmp/build-ffmpeg.sh /opt/ffmpeg

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY cmd/ ./cmd/
COPY pkg/ ./pkg/

ARG VERSION=dev
RUN PKG_CONFIG_PATH=/opt/ffmpeg/lib/pkgconfig go build \
      -ldflags "-X github.com/iSerganov/gocue/cmd/cue.version=${VERSION}" \
      -o /out/gocue .

FROM savonet/liquidsoap:v2.4.5

COPY --from=build /out/gocue /usr/local/bin/gocue

# %include "gocue.liq" resolves relative to the including script, so the
# provider and the station have to share a directory. gocue.liq is the same
# file the integration suite exercises, not a copy.
COPY integration/scripts/gocue.liq /station/gocue.liq
COPY example/scripts/station.liq   /station/station.liq

USER liquidsoap

# The base image's entrypoint is /usr/bin/liquidsoap, so this is its argument.
CMD ["/station/station.liq"]
