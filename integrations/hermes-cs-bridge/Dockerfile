# hermes-cs-bridge: GOWA (CS WhatsApp number) <-> Hermes Agent.
# Built by /opt/gowa-deploy/compose.yml on the server; see docs/cs-ai-hermes.md.
FROM golang:1.26-alpine3.23 AS builder
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY prompts ./prompts
COPY admin.html ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /out/hermes-cs-bridge .

FROM alpine:3.23
# No apk packages needed: the bridge only talks plain HTTP to local services and
# embeds timezone data (time/tzdata).
RUN addgroup -g 20002 bridge && adduser -D -H -u 20002 -G bridge bridge && \
    mkdir -p /data && chown bridge:bridge /data
COPY --from=builder /out/hermes-cs-bridge /usr/local/bin/hermes-cs-bridge
USER bridge
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["hermes-cs-bridge", "healthcheck"]
ENTRYPOINT ["hermes-cs-bridge"]
