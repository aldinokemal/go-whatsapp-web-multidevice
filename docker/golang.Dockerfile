############################
# STEP 1 build executable binary
############################
FROM golang:1.21.3-alpine3.18 AS builder
RUN apk update && apk add --no-cache vips-dev gcc musl-dev gcompat ffmpeg
WORKDIR /whatsapp
COPY ./src .

# Fetch dependencies.
RUN go mod download
# Install pkger
RUN go install github.com/markbates/pkger/cmd/pkger@latest
# Build the binary.
RUN pkger
RUN go build -o /app/whatsapp


FROM builder AS dev
RUN go install github.com/cosmtrek/air@latest
RUN go mod tidy
RUN go mod download
RUN go mod tidy -e

CMD ["air", "-c", ".air.toml"]


#############################
## STEP 2 build a smaller image
#############################
FROM alpine:3.18 as runtime
RUN apk update && apk add --no-cache vips ffmpeg
COPY ./docs /docs
WORKDIR /app
# Copy compiled from builder.
COPY --from=builder /app/whatsapp /app/whatsapp
# Run the binary.
ENTRYPOINT ["/app/whatsapp"]
