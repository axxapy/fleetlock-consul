# build environment
FROM golang:1.22-alpine AS build-env

ADD . /src
RUN cd /src && go build -o fleetlock-consul cmd/server/main.go

# run environment
FROM alpine

# https://support.circleci.com/hc/en-us/articles/360016505753-Resolve-Certificate-Signed-By-Unknown-Authority-error-in-Alpine-images
RUN apk --no-cache add ca-certificates

COPY --from=build-env /src/fleetlock-consul /usr/local/bin/fleetlock-consul

USER nobody

CMD [ "/usr/local/bin/fleetlock-consul" ]
