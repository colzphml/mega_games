FROM golang:alpine as build
ENV TZ=Europe/Moscow

ARG VERSION
ARG COMMIT

RUN apk --no-cache add tzdata

WORKDIR /app

ADD ./cmd/*.go ./cmd/
ADD ./internal/ ./internal/
ADD ./pkg/ ./pkg/

COPY go.mod go.sum ./
RUN go mod download && go mod verify

WORKDIR /app/cmd

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags "-X github.com/colzphml/mega_games/internal/app.buildVersion=$VERSION \
            -X 'github.com/colzphml/mega_games/internal/app.buildDate=$(date '+%Y-%m-%d %H:%M:%S')' \
            -X github.com/colzphml/mega_games/internal/app.buildCommit=$COMMIT" \
  -a -installsuffix cgo -o app .

FROM alpine:latest as final
ENV TZ=Europe/Moscow

COPY --from=build /app/cmd/app /mega/cmd/
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
# COPY --from=build /app/cmd/config.yaml /mega/cmd/

WORKDIR /mega/cmd

ENTRYPOINT ["./app"]