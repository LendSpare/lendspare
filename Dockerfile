# LendSpare as a container (Docker only: it needs the engine's socket to start the ComfyUI and
# pipeline containers beside itself). See compose.yaml.
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /lendspare ./cmd/lendspare

FROM docker:27-cli
COPY --from=build /lendspare /lendspare
ENV LENDSPARE_CONFIG_DIR=/config
ENTRYPOINT ["/lendspare"]
