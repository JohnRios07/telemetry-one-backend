FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /server ./cmd/api

FROM alpine:3.20

RUN apk add --no-cache ca-certificates curl

COPY --from=builder /server /server

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fsS http://localhost:8080/health || exit 1

CMD ["/server"]
