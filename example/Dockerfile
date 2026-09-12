# Builds gocue from this repository and drops it into the official Liquidsoap
# image next to the production autocue provider.
#
# The build context is the repository root, not example/:
#   docker build -f example/Dockerfile -t gocue-station ..

FROM golang:1.26 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY cmd/ ./cmd/
COPY pkg/ ./pkg/

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
      -ldflags "-X github.com/iSerganov/gocue/cmd/cue.version=${VERSION}" \
      -o /out/gocue .

FROM savonet/liquidsoap:v2.4.5

# gocue shells out to ffmpeg and ffprobe. The Liquidsoap image links the ffmpeg
# libraries but ships neither binary.
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/gocue /usr/local/bin/gocue

# %include "gocue.liq" resolves relative to the including script, so the
# provider and the station have to share a directory. gocue.liq is the same
# file the integration suite exercises, not a copy.
COPY integration/scripts/gocue.liq /station/gocue.liq
COPY example/scripts/station.liq   /station/station.liq

USER liquidsoap

# The base image's entrypoint is /usr/bin/liquidsoap, so this is its argument.
CMD ["/station/station.liq"]
