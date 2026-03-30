############################
# STEP 1 build executable binary
############################
FROM golang:1.25-alpine3.23 AS builder
RUN apk add --no-cache gcc musl-dev gcompat
WORKDIR /whatsapp

# Cache dependencies — only re-downloads when go.mod/go.sum change
COPY ./src/go.mod ./src/go.sum ./
RUN go mod download

# Copy source and build
COPY ./src .
RUN go build -ldflags="-w -s" -o /app/whatsapp

#############################
## STEP 2 build a smaller image
#############################
FROM alpine:3.23
RUN apk add --no-cache ffmpeg libwebp-tools poppler-utils tzdata su-exec

# BusyBox adduser rejects uid == gid when that gid already exists as a group; use distinct ids.
# Host bind mounts (if entrypoint cannot chown): chown -R 20001:20000 storages statics
ARG APP_UID=20001
ARG APP_GID=20000
RUN addgroup -g "${APP_GID}" gowa && \
	adduser -D -u "${APP_UID}" -G gowa -h /app gowauser

ENV TZ=UTC
WORKDIR /app

# Copy compiled from builder.
COPY --from=builder /app/whatsapp /app/whatsapp
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh && chown -R gowauser:gowa /app

# Root only for entrypoint (ownership fix on volumes); process becomes gowauser.
USER root
ENTRYPOINT ["/entrypoint.sh"]
CMD [ "rest" ]