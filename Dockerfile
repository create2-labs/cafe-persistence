FROM golang:1.26.6 AS ci
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go test ./...

FROM golang:1.26.6-bookworm AS builder

ENV CGO_ENABLED=0

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o persistence ./cmd/persistence/main.go && \
    go build -o healthcheck ./cmd/healthcheck/main.go

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app

COPY --from=builder /app/persistence /app/persistence
COPY --from=builder /app/healthcheck /app/healthcheck
COPY --from=builder /app/config.yaml /app/config.yaml

ENTRYPOINT ["/app/persistence"]
