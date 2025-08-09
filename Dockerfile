# Build stage
FROM golang:1.24-alpine AS build
WORKDIR /src

# Install git for go modules and ca-certificates
RUN apk add --no-cache git ca-certificates

COPY go.mod .
COPY main.go .
RUN go env -w GOPROXY=https://proxy.golang.org
RUN go build -o /bin/wake-on-lan-proxy

# Runtime image
FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /bin/wake-on-lan-proxy /usr/local/bin/wake-on-lan-proxy

EXPOSE ${LISTEN_ADDR}
ENTRYPOINT ["/usr/local/bin/wake-on-lan-proxy"]

