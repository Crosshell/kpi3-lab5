FROM golang:1.24 AS builder

WORKDIR /app
COPY . .

# Build all components
RUN CGO_ENABLED=0 go build -o bin/db ./cmd/db
RUN CGO_ENABLED=0 go build -o bin/server ./cmd/server
RUN CGO_ENABLED=0 go build -o bin/balancer ./cmd/lb/balancer.go

# ==== Final image ====
FROM alpine:latest
WORKDIR /opt/practice-4

# Install curl for healthchecks
RUN apk add --no-cache curl

# Create data directory
RUN mkdir -p /opt/practice-4/out && chmod -R 777 /opt/practice-4/out

# Copy binaries
COPY --from=builder /app/bin/ /opt/practice-4/

# Copy entry script
COPY entry.sh /opt/practice-4/
RUN chmod +x /opt/practice-4/entry.sh
RUN chmod +x /opt/practice-4/*

ENTRYPOINT ["/opt/practice-4/entry.sh"]