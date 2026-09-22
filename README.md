# Forge

Forge is a personal, local-first platform-engineering project built primarily
in Go. It is a place to explore what happens between writing an application and
operating it as a dependable system.

The project is inspired by developer platforms such as Heroku, Railway, Render,
and internal Kubernetes platforms, but it is not intended to compete with them.
The interesting part of Forge is understanding and eventually building some of
the machinery hidden behind their simple interfaces.

## The Core Idea

Modern platforms let a developer deploy software with a small command while
hiding processes, networks, containers, schedulers, health checks, logs, and
recovery behavior underneath.

Forge explores those layers from the bottom up:

```text
Go binary
    ↓
Linux process
    ↓
service manager
    ↓
container
    ↓
orchestrator
    ↓
developer platform
```

Each layer begins as something operated and inspected directly. Automation is
introduced after the underlying mechanism is understood and there is a real
reason to hide its repetitive details.

The long-term direction is a small interface such as:

```sh
forge deploy hello-api
forge status hello-api
forge logs hello-api
forge stop hello-api
```

Those commands are not the whole project. The project is everything required to
make them work—and being able to explain how that system behaves when it works,
when it fails, and when it needs to recover.

Forge is not a formal course or a checklist of infrastructure tools. It grows
through personal interest, working experiments, and problems that make the next
abstraction useful. The application itself stays deliberately small so the
infrastructure around it remains the main subject.

## Current State

Milestone 0 provides `examples/hello-api`, a small workload that gives Forge
something real to operate. It includes:

- an HTTP endpoint and health check;
- environment-based port configuration;
- graceful `SIGINT` and `SIGTERM` handling;
- unit tests;
- a multi-stage container build;
- an unprivileged container runtime user.

Milestone 1 adds the first `forge` CLI. It builds, starts, inspects, logs, stops,
and deletes the `hello-api` container through explicit Docker operations.

Milestone 2 makes deployment health-aware. Forge waits for `/healthz` before
reporting success, times out failed starts, preserves failed containers for
inspection, and includes application health in `forge status`.

Milestone 3 adds continuous integration through GitHub Actions. Pushes to
`main` and pull requests verify formatting, tests, static analysis, the Forge
CLI build, the `hello-api` image build, and a live container health check.

Milestone 4 publishes successful `main` builds to GitHub Container Registry.
Each image is tagged with its Git commit SHA and supports both `linux/amd64`
and `linux/arm64`. Forge can pull and deploy that exact artifact instead of
rebuilding source on the deployment machine.

## Forge CLI

Build the CLI:

```sh
go build -o bin/forge ./cmd/forge
```

Deploy the example workload on the default host port `8080`:

```sh
./bin/forge deploy hello-api
```

Choose another host port when needed:

```sh
./bin/forge deploy --port 18080 hello-api
```

Deploy an immutable image previously published by CI:

```sh
./bin/forge deploy \
  --port 18080 \
  --image ghcr.io/nizamiqarayev/forge-hello-api:<commit-sha> \
  hello-api
```

Without `--image`, Forge builds `hello-api:local` from the current checkout.
With `--image`, Forge pulls the supplied image and deploys it without a local
rebuild.

Operate the deployed workload:

```sh
./bin/forge status hello-api
./bin/forge logs hello-api
./bin/forge stop hello-api
./bin/forge delete hello-api
```

Forge names the container `forge-hello-api` and labels it with
`forge.managed=true` and `forge.app=hello-api`. Deploy refuses to replace an
existing container; stop and delete it explicitly before deploying again. A
deployment is successful only after the workload returns HTTP 200 from
`/healthz` within 15 seconds. `forge status` reports the exact image reference
stored in the running container.

## Continuous Integration and Images

The workflow in `.github/workflows/ci.yml` runs on pull requests and pushes to
`main`. Pull requests perform verification only. A successful `main` run also
publishes:

```text
ghcr.io/nizamiqarayev/forge-hello-api:<commit-sha>
```

The commit tag is immutable deployment input: it connects a running container
to the exact source revision that produced it. The registry tag points to a
multi-platform manifest, allowing Docker to select AMD64 on common Linux
servers or ARM64 on Apple Silicon automatically.

`hello-api` remains Forge's controlled integration workload while the platform
is being built and tested. Once repository-driven deployments are supported,
Malcore will be the first real external project operated through Forge.

## Run the Service

Run directly from source:

```sh
go run ./examples/hello-api
```

Or build and run the executable:

```sh
go build -o bin/hello-api ./examples/hello-api
./bin/hello-api
```

The service listens on port `8080` by default. Set `PORT` to choose another
port:

```sh
PORT=9000 ./bin/hello-api
```

Try its endpoints:

```sh
curl http://localhost:8080/
curl http://localhost:8080/healthz
```

Stop the process with `Ctrl-C`. The service handles the signal and performs a
graceful shutdown.

## Run in Docker

Build the image:

```sh
docker build \
  -f examples/hello-api/Dockerfile \
  -t hello-api:local \
  .
```

Run the workload in the foreground and publish its HTTP port to the Mac:

```sh
docker run --rm --name hello-api -p 8080:8080 hello-api:local
```

From another terminal, inspect and use the running container:

```sh
docker top hello-api
docker logs hello-api
curl http://localhost:8080/healthz
docker exec hello-api cat /proc/1/status
```

Stop it gracefully:

```sh
docker stop --timeout 10 hello-api
```

The Go binary runs directly as PID 1 inside the container. Docker sends it
`SIGTERM` when stopping the container, allowing the service's existing graceful
shutdown path to run. The final image uses a small Debian runtime and runs the
process as an unprivileged `hello-api` user.

## Verify the Code

```sh
go test ./...
go vet ./...
```

## Project Notes

The broader direction, possible architecture, and areas of interest are
documented in [the project vision](docs/PROJECT_VISION.md).
