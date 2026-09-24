##############################
# Stage CI — Lint / Test / Vuln (use: docker build --target ci)
##############################
FROM golang:1.26.6 AS ci

WORKDIR /app

# Cache-bust: force reinstall of tools when Go version changes (must match image tag)
ENV GO_TOOLING_VERSION=1.26.6
ENV PATH=/go/bin:/usr/local/go/bin:/usr/local/bin:$PATH

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go install golang.org/x/vuln/cmd/govulncheck@latest \
    && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0

CMD ["sh", "-c", "go mod download && golangci-lint run ./... && go test ./... && govulncheck ./..."]


##############################
# Stage Build
##############################
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
