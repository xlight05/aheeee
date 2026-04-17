# syntax=docker/dockerfile:1.6

FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

ARG VERSION=1.0.0
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build \
    -trimpath \
    -ldflags="-s -w -X main.defaultVersion=${VERSION}" \
    -o /out/hello-web \
    .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/hello-web /app/hello-web

USER nonroot:nonroot
EXPOSE 9090
ENV PORT=9090

ENTRYPOINT ["/app/hello-web"]
