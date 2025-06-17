# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o monitor

# Final stage
FROM alpine:latest

WORKDIR /app

# Install common Unix tools and utilities
RUN apk add --no-cache \
    bash \
    curl \
    jq \
    sed \
    grep \
    coreutils \
    util-linux \
    procps \
    findutils \
    gawk \
    && rm -rf /var/cache/apk/*

RUN apk add --no-cache curl bash && \
curl https://clickhouse.com/ | sh

# Copy the binary from builder
COPY --from=builder /app/monitor .

# Copy config file
COPY config.yaml .

# Expose metrics port
EXPOSE 2112

# Run the application
CMD ["./monitor"] 