FROM golang:1.24-alpine AS build

RUN apk add --no-cache gzip

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/server ./cmd/server
RUN mkdir -p /app/resources && gzip -dkc resources/index.bin.gz > /app/resources/index.bin

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=build /app/server /app/server
COPY --from=build /app/resources/index.bin /app/resources/index.bin

ENV INDEX_PATH=/app/resources/index.bin
ENV LISTEN_TCP=0
ENV IVF_NPROBE=5
ENV IVF_FULL_NPROBE=24
ENV CANDIDATES=0

ENTRYPOINT ["/app/server"]
