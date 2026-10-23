FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS builder
ARG TARGETOS TARGETARCH TARGETVARIANT
WORKDIR /build
COPY go.mod ./
COPY . .
RUN GOARM=$(echo "$TARGETVARIANT" | tr -d 'v') \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -o bmw-bridge ./cmd/main.go

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/bmw-bridge .
VOLUME /data
EXPOSE 8080
ENV REST_PORT=8080
ENV POLL_INTERVAL_MINUTES=60
ENV DATA_DIR=/data
ENTRYPOINT ["./bmw-bridge"]
