FROM golang:1.25-alpine3.23
RUN apk update && apk add --no-cache gcc musl-dev gcompat ffmpeg libwebp-tools tzdata
ENV TZ=UTC
ENV CGO_ENABLED=1
WORKDIR /app
RUN go install github.com/air-verse/air@latest
WORKDIR /app/src
CMD ["air", "-c", ".air.toml"]
