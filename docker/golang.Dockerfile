############################
# STEP 1 build executable binary
############################
FROM golang:1.21.5-alpine3.19 AS builder
RUN apk update && apk add --no-cache gcc musl-dev gcompat
WORKDIR /whatsapp
COPY ./src .

# Fetch dependencies.
RUN go mod download
# Build the binary.
RUN go build -o /app/whatsapp

#############################
## STEP 2 build a smaller image
#############################
FROM alpine:3.19
RUN apk update && apk add --no-cache ffmpeg
WORKDIR /app
# Copy compiled from builder.
COPY --from=builder /app/whatsapp /app/whatsapp

# Set default environment variables for port and webhook
ENV PORT 3000
ENV WEBHOOK "http://localhost:3000/handler"

# Use shell form to ensure environment variables are evaluated
CMD ["sh", "-c", "/app/whatsapp -p ${PORT} -w=${WEBHOOK}"]
CMD ["/bin/sh", "-c", "/app/whatsapp --port ${PORT} -w=${WEBHOOK}"]
