# Final stage with ClamAV
FROM alpine:latest

ARG TARGETARCH

ENV APP_NAME=clamav-api
ENV APP_ENV=production
ENV CLAMAV_PORT=6000
ENV CLAMAV_GRPC_PORT=9000
ENV CLAMAV_SOCKET=/var/run/clamav/clamd.ctl

# Install required packages
RUN apk add --no-cache \
    supervisor \
    clamav \
    clamav-libunrar \
    dcron \
    ca-certificates

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

# Copy pre-built binary from bin directory
WORKDIR /app
COPY bin/clamav-api-linux-${TARGETARCH} /app/clamav-api
RUN chown clamav-api:clamav-api /app/clamav-api && \
    chmod +x /app/clamav-api

# Note: ClamAV database updates are handled at runtime by:
# - docker-entrypoint.sh (initial update if needed)
# - cron job (scheduled updates at 3 AM daily)
# This ensures fresh virus definitions and deterministic builds

# Declare volumes for persistence
VOLUME ["/var/lib/clamav", "/var/run/clamav"]

WORKDIR /
EXPOSE 6000 9000

ENTRYPOINT ["./docker-entrypoint.sh"]
