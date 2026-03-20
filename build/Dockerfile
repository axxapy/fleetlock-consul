FROM golang:1.26-alpine AS build-env

ADD . /src
RUN cd /src && CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o fleetlock-consul ./cmd/server

FROM scratch

COPY --from=build-env /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build-env /src/fleetlock-consul /usr/local/bin/fleetlock-consul

USER 65534

ENTRYPOINT ["/usr/local/bin/fleetlock-consul"]
