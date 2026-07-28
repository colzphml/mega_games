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

RUN apk add --no-cache tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN set -eu; \
    GOOS="${TARGETOS:-linux}"; \
    GOARCH="${TARGETARCH:-amd64}"; \
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -o /out/discord-kafka-telegram-week-sender \
    -ldflags "-s -w -X main.buildVersion=${VERSION:-dev} -X main.buildCommit=${COMMIT:-unknown} -X main.buildDate=${BUILD_DATE:-unknown}" \
    ./discord_kafka_telegram_week_sender/cmd/discord-kafka-telegram-week-sender

FROM alpine:3.21

ARG TZ=Europe/Moscow
ENV TZ=$TZ

RUN apk add --no-cache tzdata

RUN adduser -D -g '' app
USER app
WORKDIR /app

COPY --from=build /out/discord-kafka-telegram-week-sender /app/discord-kafka-telegram-week-sender

ENTRYPOINT ["/app/discord-kafka-telegram-week-sender"]
