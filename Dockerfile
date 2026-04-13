FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download

COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/tagmanager .

FROM alpine:3.20
RUN adduser -D -g "" appuser
USER appuser
WORKDIR /app
COPY --from=builder /out/tagmanager /usr/local/bin/tagmanager

ENV HTTP_ADDR=:8080
ENV SAYMON_CONFIG_PATH=/etc/saymon/saymon-server.conf
ENV TAGS_COLLECTION=tags

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/tagmanager"]
