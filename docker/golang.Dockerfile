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
RUN apk add --no-cache ffmpeg libwebp-tools poppler-utils tzdata

# Security: run as non-root
RUN adduser -D -h /app gowauser
ENV TZ=UTC
WORKDIR /app

# Copy compiled from builder.
COPY --from=builder /app/whatsapp /app/whatsapp
RUN chown -R gowauser:gowauser /app

USER gowauser
ENTRYPOINT ["/app/whatsapp"]
CMD [ "rest" ]