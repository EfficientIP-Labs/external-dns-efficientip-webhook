#Stage 1: Build stage
FROM golang:1.23 AS builder

WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application code
COPY . ./

# Build the Go binary with static linking
RUN CGO_ENABLED=0 go build -a -o /app/bin/external-dns-efficientip-webhook ./cmd/webhook/main.go

# Stage 2: Runtime stage
FROM debian:bullseye-slim

WORKDIR /app

# Install CA certificates
RUN apt-get update && apt-get install -y ca-certificates && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

# Copy the statically linked binary from the builder stage
COPY --from=builder /app/bin/external-dns-efficientip-webhook /app/bin/external-dns-efficientip-webhook

# Set the binary as the entry point
CMD ["/app/bin/external-dns-efficientip-webhook"]
