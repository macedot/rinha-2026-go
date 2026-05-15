FROM golang:1.24-alpine AS build

RUN apk add --no-cache gcc musl-dev gzip

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -ldflags="-s -w -extldflags '-static'" -o /app/server ./cmd/server
RUN mkdir -p /app/resources && gzip -dkc bridge/data/index.bin.gz > /app/resources/index.bin

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=build /app/server /app/server
COPY --from=build /app/resources/index.bin /app/resources/index.bin
COPY --from=build /src/resources/mcc_risk.json /app/resources/mcc_risk.json

ENV INDEX_PATH=/app/resources/index.bin
ENV MCC_RISK_PATH=/app/resources/mcc_risk.json
ENV LISTEN_TCP=0
ENV IVF_NPROBE=32
ENV IVF_FULL_NPROBE=8
ENV CANDIDATES=0

ENTRYPOINT ["/app/server"]
