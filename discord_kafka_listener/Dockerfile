FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build

SHELL ["/bin/sh", "-c"]

ARG TARGETOS
ARG TARGETARCH
ARG VERSION
ARG COMMIT
ARG BUILD_DATE

WORKDIR /src

RUN apk add --no-cache git tzdata

COPY go.mod ./
RUN go mod download

COPY . ./
RUN VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo unknown)}" \
    COMMIT="${COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}" \
    BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}" \
    GOOS="${TARGETOS:-linux}" \
    GOARCH="${TARGETARCH:-amd64}" \
    CGO_ENABLED=0 \
    go build -o /out/discord-kafka-listener \
    -ldflags "-s -w \
      -X main.buildVersion=${VERSION} \
      -X main.buildCommit=${COMMIT} \
      -X main.buildDate=${BUILD_DATE}" \
    ./cmd/discord-kafka-listener

FROM alpine:3.20

RUN adduser -D -g '' app
USER app
WORKDIR /app

COPY --from=build /out/discord-kafka-listener /app/discord-kafka-listener

ENTRYPOINT ["/app/discord-kafka-listener"]
