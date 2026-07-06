# Builds Concord's rendezvous/relay node for deployment (e.g. fly.io).
# Build context is the repo root so the vendored third_party/mls-go replace
# directive resolves.
FROM golang:1.26 AS build
WORKDIR /src
COPY . .
# Pure-Go build (no cgo): the rendezvous node uses only libp2p + the DHT.
RUN CGO_ENABLED=0 go build -trimpath -o /rendezvous ./cmd/rendezvous

FROM gcr.io/distroless/static-debian12
COPY --from=build /rendezvous /rendezvous
ENV PORT=4001
EXPOSE 4001/tcp
EXPOSE 4001/udp
ENTRYPOINT ["/rendezvous"]
