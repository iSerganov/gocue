# Builds gocue from this repository and drops it into the official Liquidsoap
# image next to the production autocue provider.
#
# The build context is the repository root, not example/:
#   docker build -f example/Dockerfile -t gocue-station ..

FROM golang:1.26 AS build

# gocue links libav through cgo. Liquidsoap's image carries FFmpeg 7.1, so
# gocue gets its own FFmpeg 9.0.1, the version the tests run against.
ARG TARGETARCH
RUN apt-get update \
 && apt-get install -y --no-install-recommends xz-utils \
 && rm -rf /var/lib/apt/lists/* \
 && case "$TARGETARCH" in \
      amd64) arch=linux64 ;; \
      arm64) arch=linuxarm64 ;; \
      *) echo "no FFmpeg 9.0.1 build for $TARGETARCH" >&2; exit 1 ;; \
    esac \
 && mkdir -p /opt/ffmpeg \
 && curl -fsSL "https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-08-31-13-27/ffmpeg-n9.0.1-11-ge47273f4d9-${arch}-gpl-shared-9.0.tar.xz" \
    | tar -xJ --strip-components=1 -C /opt/ffmpeg

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY cmd/ ./cmd/
COPY pkg/ ./pkg/

ARG VERSION=dev
RUN PKG_CONFIG_PATH=/opt/ffmpeg/lib/pkgconfig \
    CGO_LDFLAGS="-Wl,-rpath,/opt/ffmpeg/lib -Wl,--disable-new-dtags" \
    go build \
      -ldflags "-X github.com/iSerganov/gocue/cmd/cue.version=${VERSION}" \
      -o /out/gocue .

FROM savonet/liquidsoap:v2.4.5

# gocue loads these libraries through its rpath and still runs the ffmpeg
# binary for the loudness scan. Liquidsoap keeps using its own FFmpeg 7.1.
COPY --from=build /opt/ffmpeg/lib /opt/ffmpeg/lib
COPY --from=build /opt/ffmpeg/bin/ffmpeg /opt/ffmpeg/bin/ffmpeg
ENV PATH=/opt/ffmpeg/bin:$PATH

COPY --from=build /out/gocue /usr/local/bin/gocue

# %include "gocue.liq" resolves relative to the including script, so the
# provider and the station have to share a directory. gocue.liq is the same
# file the integration suite exercises, not a copy.
COPY integration/scripts/gocue.liq /station/gocue.liq
COPY example/scripts/station.liq   /station/station.liq

USER liquidsoap

# The base image's entrypoint is /usr/bin/liquidsoap, so this is its argument.
CMD ["/station/station.liq"]
