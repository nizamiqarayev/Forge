# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS build

WORKDIR /src

COPY *.go ./
RUN CGO_ENABLED=0 go build -o /out/hello-api main.go

FROM debian:bookworm-slim AS runtime

RUN groupadd --system hello-api \
    && useradd --system --gid hello-api --no-create-home hello-api

COPY --from=build --chown=hello-api:hello-api /out/hello-api /usr/local/bin/hello-api

USER hello-api
EXPOSE 8080
ENV PORT=8080

ENTRYPOINT ["/usr/local/bin/hello-api"]
