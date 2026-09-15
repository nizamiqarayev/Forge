# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS build

WORKDIR /src

# Copy the module definition separately so dependency downloads can be cached.
COPY go.mod ./
RUN go mod download

COPY examples/hello-api ./examples/hello-api
RUN CGO_ENABLED=0 go build -o /out/hello-api ./examples/hello-api

FROM debian:bookworm-slim AS runtime

RUN groupadd --system hello-api \
    && useradd --system --gid hello-api --no-create-home hello-api

COPY --from=build --chown=hello-api:hello-api /out/hello-api /usr/local/bin/hello-api

USER hello-api
EXPOSE 8080
ENV PORT=8080

ENTRYPOINT ["/usr/local/bin/hello-api"]
