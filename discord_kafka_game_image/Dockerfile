FROM --platform=$BUILDPLATFORM golang:1.25.5-alpine AS build

SHELL ["/bin/sh", "-c"]

ARG TARGETOS
ARG TARGETARCH
ARG VERSION
ARG COMMIT
ARG BUILD_DATE
ARG TZ=Europe/Moscow

ENV TZ=$TZ

WORKDIR /src

RUN apk add --no-cache git tzdata

COPY .git ./.git

WORKDIR /src/discord_kafka_game_image
COPY discord_kafka_game_image/go.mod discord_kafka_game_image/go.sum ./
RUN go mod download

COPY discord_kafka_game_image/ ./
RUN set -eu; \
    VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo unknown)}"; \
    COMMIT="${COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"; \
    BUILD_DATE="${BUILD_DATE:-$(date +%Y-%m-%dT%H:%M:%S%z)}"; \
    GOOS="${TARGETOS:-linux}"; \
    GOARCH="${TARGETARCH:-amd64}"; \
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -o /out/discord-kafka-game-image \
    -ldflags "-s -w -X main.buildVersion=${VERSION} -X main.buildCommit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    ./cmd/discord-kafka-game-image

FROM alpine:3.21

ARG TZ=Europe/Moscow
ENV TZ=$TZ

RUN apk add --no-cache tzdata chromium nss freetype harfbuzz ttf-freefont

RUN adduser -D -g '' app
USER app
WORKDIR /app

COPY --from=build /out/discord-kafka-game-image /app/discord-kafka-game-image

ENTRYPOINT ["/app/discord-kafka-game-image"]
