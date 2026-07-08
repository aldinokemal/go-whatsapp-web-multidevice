FROM aldinokemal2104/go-whatsapp-web-multidevice:latest
USER root
RUN apk add --no-cache gcc musl-dev go git make
WORKDIR /build
COPY ./src /build
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /app/whatsapp .
USER gowauser
ENTRYPOINT ["/entrypoint.sh"]
CMD ["rest"]
