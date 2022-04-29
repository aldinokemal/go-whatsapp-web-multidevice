############################
# STEP 1 build executable binary
############################
FROM golang:alpine AS builder
RUN apk update && apk add --no-cache vips-dev gcc musl-dev gcompat
WORKDIR /whatsapp
COPY ./src .

# Fetch dependencies.
RUN go mod download
# Build the binary.
RUN go build -o app

#############################
## STEP 2 build a smaller image
#############################
FROM alpine
RUN apk update && apk add --no-cache vips-dev
WORKDIR /whatsapp
# Copy compiled from builder.
COPY --from=builder /whatsapp .
# Run the binary.
ENTRYPOINT ["./app"]