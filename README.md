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
forge deploy hello
forge status hello
forge logs hello
forge stop hello
```

Those commands are not the whole project. The project is everything required to
make them work—and being able to explain how that system behaves when it works,
when it fails, and when it needs to recover.

Forge is not a formal course or a checklist of infrastructure tools. It grows
through personal interest, working experiments, and problems that make the next
abstraction useful. The application itself stays deliberately small so the
infrastructure around it remains the main subject.

## Current Focus

The repository currently contains a small HTTP service that gives Forge
something real to operate. The immediate goal is to run it as a Linux process
and inspect:

- its PID and parent process;
- environment and file descriptors;
- the socket listening on its HTTP port;
- stdout and stderr;
- signal delivery and graceful shutdown.

Once manual process operation is familiar, a service manager can take
responsibility for starting, stopping, restarting, and logging the same binary.

## Run the Service

Run directly from source:

```sh
go run ./cmd/forge
```

Or build and run the executable:

```sh
go build -o bin/forge ./cmd/forge
./bin/forge
```

The service listens on port `8080` by default. Set `PORT` to choose another
port:

```sh
PORT=9000 ./bin/forge
```

Try its endpoints:

```sh
curl http://localhost:8080/
curl http://localhost:8080/healthz
```

Stop the process with `Ctrl-C`. The service handles the signal and performs a
graceful shutdown.

## Verify the Code

```sh
go test ./...
go vet ./...
```

## Project Notes

The broader direction, possible architecture, and areas of interest are
documented in [the project vision](docs/PROJECT_VISION.md).
