# Build stage for Go API
FROM --platform=$BUILDPLATFORM golang:1.20-alpine AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# Copy the src directory containing go.mod and main.go
COPY src/ ./

# Download dependencies
RUN go mod download

# Build the application with proper GOOS and GOARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o clamav-api main.go

# Final stage
FROM --platform=$TARGETPLATFORM alpine:latest

# Install required packages
RUN apk add --no-cache \
    supervisor \
    clamav \
    clamav-libunrar \
    dcron

# Create clamav-api user and group
RUN addgroup -S clamav-api && \
    adduser -S -G clamav-api clamav-api && \
    adduser clamav-api clamav

# Configure clamavd
RUN mkdir -p /var/run/clamav && \
    mkdir -p /var/log/clamav && \
    chown clamav:clamav /var/run/clamav && \
    chown clamav:clamav -R /var/lib/clamav && \
    chown clamav:clamav /var/log/clamav && \
    chmod 750 /var/run/clamav

# Configure cron for freshclam updates
RUN mkdir -p /opt/cron/periodic && \
    mkdir -p /opt/cron/crontabs && \
    mkdir -p /opt/cron/cronstamps && \
    echo "0 3 * * * /usr/bin/freshclam >> /var/log/freshclam.log 2>&1" > /opt/cron/crontabs/root && \
    chown -R clamav:clamav /opt/cron && \
    chmod 755 /usr/sbin/crond

# Copy configurations
COPY docker/clamavd/config/clamav /etc/clamav/
COPY docker/common/config/supervisord.conf /etc/supervisord.conf
COPY docker/clamavd/config/supervisord /etc/supervisor/conf.d/

# Copy init scripts and set permissions
COPY docker/common/init /init/
COPY docker/docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh /init/* && \
    chown -R clamav-api:clamav-api /init /docker-entrypoint.sh && \
    chmod 755 /usr/bin/freshclam && \
    chmod 755 /usr/sbin/clamd

# Copy the Go API from builder stage
COPY --from=builder /app/clamav-api /app/clamav-api
RUN chown clamav-api:clamav-api /app/clamav-api

# Initial ClamAV database update
RUN freshclam

# Declare volumes for persistence
VOLUME ["/var/lib/clamav", "/var/run/clamav"]

WORKDIR /
EXPOSE 6000

ENTRYPOINT ["./docker-entrypoint.sh"]
