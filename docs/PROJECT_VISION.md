# Forge

## A Personal Platform-as-a-Service Experiment

Forge is a personal, local-first Platform-as-a-Service project built primarily in Go.

It is inspired conceptually by platforms such as:

* Heroku
* Railway
* Render
* Fly.io
* Kubernetes-based internal developer platforms

but Forge is not intended to compete with those products.

I am building it because infrastructure and platform engineering are personally interesting to me.

Forge gives me a practical way to explore how modern software infrastructure is constructed by progressively building a platform capable of deploying, operating, observing, scaling, updating, and recovering applications.

---

# The Core Idea

Most software developers interact with infrastructure through abstractions.

They might:

```bash
git push
```

and receive a deployed application.

Or run:

```bash
kubectl apply -f deployment.yaml
```

without fully understanding what Kubernetes is coordinating underneath.

Or declare:

```hcl
resource "aws_instance" "app" {
    ...
}
```

without understanding the operating system, networking, service lifecycle, or distributed-system assumptions beneath the resource.

Forge takes the opposite approach.

We begin near the bottom.

We manually run applications.

We inspect processes.

We work with ports and sockets.

We watch signals.

We configure reverse proxies.

We package applications into containers.

We automate deployment.

We introduce orchestration.

We introduce infrastructure as code.

We introduce observability.

We introduce distributed communication.

We introduce failure.

Finally, we build abstractions over everything we learned.

The result should be both:

1. a functioning developer platform,
2. and a record of hands-on infrastructure engineering exploration.

---

# Philosophy

Forge follows one central rule:

> Understand the mechanism before depending on the abstraction.

The expected progression looks like:

```text
process
    ↓
service manager
    ↓
container
    ↓
orchestrator
    ↓
platform
```

Similarly:

```text
manual infrastructure
    ↓
scripts
    ↓
Infrastructure as Code
    ↓
reusable modules
    ↓
developer platform
```

And:

```text
manual deployment
    ↓
deployment script
    ↓
CI/CD
    ↓
GitOps
```

The higher layers are not replacements for understanding.

They are abstractions built on top of mechanisms we already understand.

---

# What Forge Is Not

Forge is not primarily:

* a CRUD API project,
* a frontend project,
* a microservices demo,
* a Kubernetes tutorial,
* a Terraform tutorial,
* a Docker tutorial,
* a collection of DevOps YAML files.

The individual tools are not the project.

The system connecting them is the project.

Likewise, the example applications deployed through Forge are intentionally simple.

The application exists to give the infrastructure something real to operate.

The platform surrounding the application is the interesting engineering problem.

---

# Long-Term User Experience

Eventually a developer should be able to interact with Forge through commands such as:

```bash
forge deploy payments
```

```bash
forge status payments
```

```bash
forge logs payments
```

```bash
forge scale payments --replicas 5
```

```bash
forge rollback payments
```

```bash
forge delete payments
```

A future command might produce output such as:

```text
Building image...
Image: forge.local/payments:7c8ab91

Creating release...
Deploying revision...

Waiting for readiness...

2/2 replicas healthy

Deployment successful.

Application:
http://payments.localhost

Version:
7c8ab91

Replicas:
2

Status:
healthy
```

The simplicity of the command is intentional.

The interesting challenge is understanding everything that had to happen underneath it.

---

# Conceptual Architecture

Forge may eventually evolve toward an architecture like:

```text
                         Developer
                             │
                             │
                         forge CLI
                             │
                             ▼
                    Forge Control Plane
                             │
          ┌──────────────────┼──────────────────┐
          │                  │                  │
          ▼                  ▼                  ▼
   Deployment Engine    State / Metadata    Event System
          │               PostgreSQL           NATS
          │
          ▼
      Kubernetes
          │
    ┌─────┼─────┐
    │     │     │
    ▼     ▼     ▼
 Service Service Worker
    │
    ├──────── PostgreSQL
    ├──────── Redis
    └──────── NATS
```

The delivery system could eventually resemble:

```text
Developer
    │
    ▼
GitHub
    │
    ▼
GitHub Actions
    │
    ├── test
    ├── build
    ├── scan
    └── publish image
            │
            ▼
      Container Registry
            │
            ▼
        GitOps State
            │
            ▼
          Argo CD
            │
            ▼
        Kubernetes
```

Observability could eventually resemble:

```text
Applications
     │
     ├──── metrics ──────► Prometheus
     │                        │
     │                        ▼
     │                     Grafana
     │
     ├──── logs ─────────► Loki
     │                        │
     │                        ▼
     │                     Grafana
     │
     └──── traces ───────► Tempo
                              │
                              ▼
                           Grafana

Instrumentation:
OpenTelemetry
```

Infrastructure definitions could eventually include:

```text
Terraform
    │
    ├── cluster resources
    ├── networking
    ├── storage
    └── platform dependencies

Helm
    │
    ├── Forge components
    ├── observability stack
    └── supporting services
```

This architecture is a destination.

It should not be implemented immediately.

Every component should enter Forge because the project encounters a problem that justifies it.

---

# The First Version

The first version of Forge should be deliberately primitive.

Before building a platform, we need something to operate.

A minimal Go service might provide:

```text
GET /health
GET /ready
GET /users
POST /users
GET /metrics
```

Initially we should run the binary directly on Linux.

Architecture:

```text
Client
   │
   ▼
Caddy / Nginx / Traefik
   │
   ▼
Go Process
   │
   ▼
PostgreSQL
```

This gives us reasons to learn:

* Linux process management,
* process ownership,
* ports,
* TCP,
* HTTP,
* reverse proxies,
* environment variables,
* signals,
* logging,
* PostgreSQL connections,
* startup,
* shutdown,
* health checks.

Only after understanding that system should we containerize it.

---

# Evolution 1 — Linux Service

We begin with a compiled Go binary.

Questions we should be able to answer include:

* What is a process?
* What is a PID?
* What resources belong to a process?
* What is a file descriptor?
* How does the process listen on a TCP port?
* What happens when a connection arrives?
* How does Linux schedule the process?
* What happens when the process receives SIGTERM?
* Where do stdout and stderr go?
* What happens if the process crashes?
* Who restarts it?

Then introduce a service manager such as systemd.

Now we can explore:

* service lifecycle,
* restart policies,
* dependency ordering,
* environment configuration,
* logs,
* process supervision.

---

# Evolution 2 — Networking

Networking should be treated as foundational.

Topics include:

* network interfaces,
* IP addresses,
* ports,
* TCP,
* UDP where relevant,
* sockets,
* DNS,
* HTTP,
* HTTPS,
* TLS,
* routing,
* NAT,
* loopback,
* reverse proxies,
* load balancing.

We should follow a request through the system.

For example:

```text
curl
 │
 ▼
DNS resolution
 │
 ▼
TCP connection
 │
 ▼
HTTP request
 │
 ▼
reverse proxy
 │
 ▼
upstream TCP connection
 │
 ▼
Go HTTP server
 │
 ▼
handler
```

Eventually the same mental model should still apply inside Kubernetes.

---

# Evolution 3 — Containers

Once the manually operated system is understood, package it into containers.

This stage should explore both how to use containers and why they work.

Topics include:

* images,
* image layers,
* Dockerfiles,
* registries,
* containers,
* Linux namespaces,
* cgroups,
* filesystem isolation,
* networking,
* volumes,
* PID 1,
* signals,
* resource limits,
* multi-stage builds.

Example:

```text
Go source
   │
   ▼
Build stage
   │
   ▼
Go binary
   │
   ▼
Runtime image
   │
   ▼
Container
```

Docker Compose can initially coordinate:

```text
reverse proxy
Go service
PostgreSQL
Redis
NATS
```

This creates the first reproducible local environment.

---

# Evolution 4 — Forge CLI

Once deployments contain enough repetitive manual operations, begin creating the actual Forge CLI.

The first version might simply automate Docker operations.

Example:

```bash
forge deploy ./examples/hello-api
```

Internally:

```text
read configuration
      │
      ▼
build image
      │
      ▼
create container
      │
      ▼
configure networking
      │
      ▼
wait for health check
      │
      ▼
report status
```

Additional commands might include:

```bash
forge status hello-api
forge logs hello-api
forge stop hello-api
forge delete hello-api
```

This begins the transition from:

> manually operating infrastructure

to:

> writing software that operates infrastructure.

This is a core objective of Forge.

---

# Go's Role

Go is central to Forge because it is useful for infrastructure tooling and gives access to the mechanisms we want to study.

Forge should naturally expose us to:

```text
net/http
net
context
os
os/exec
os/signal
syscall
io
io/fs
sync
time
encoding/json
testing
```

and later:

* gRPC
* OpenTelemetry
* Kubernetes APIs
* Prometheus clients
* structured logging
* configuration libraries where justified.

We should especially focus on:

* concurrency,
* cancellation,
* deadlines,
* resource management,
* network communication,
* process management,
* safe shutdown,
* streaming,
* observability.

Go code should support the infrastructure project rather than becoming the entire project.

---

# Evolution 5 — Delivery Automation

Manual builds and deployments eventually become repetitive and error-prone.

Introduce CI/CD.

A pipeline might evolve toward:

```text
commit
   │
   ▼
tests
   │
   ▼
build image
   │
   ▼
security checks
   │
   ▼
publish image
   │
   ▼
deploy staging
   │
   ▼
smoke test
   │
   ▼
promote
```

Concepts include:

* build reproducibility,
* artifact immutability,
* image tagging,
* commit SHA versions,
* environments,
* secrets,
* rollback,
* deployment strategies.

GitHub Actions is a suitable free learning environment.

The pipeline should automate procedures we previously performed manually.

---

# Evolution 6 — Kubernetes

Eventually Docker Compose will expose limitations.

For example:

* Who keeps the desired replica count?
* Who replaces crashed containers?
* Who decides where workloads run?
* How do services find each other?
* How are rolling updates performed?
* How are health checks connected to traffic routing?
* How do we scale?
* What happens when a node disappears?

Kubernetes becomes relevant because these problems now exist.

Start locally with:

* kind, or
* k3d.

Learn the Kubernetes reconciliation model.

Important resources include:

```text
Pod
Deployment
ReplicaSet
Service
Ingress
ConfigMap
Secret
Namespace
PersistentVolume
PersistentVolumeClaim
```

Important runtime concepts include:

* desired state,
* reconciliation,
* controllers,
* scheduling,
* readiness,
* liveness,
* resource requests,
* resource limits,
* rolling updates,
* service discovery,
* DNS.

Forge can eventually interact with Kubernetes through Go.

For example:

```text
forge deploy
     │
     ▼
Forge control plane
     │
     ▼
Kubernetes API
     │
     ▼
Deployment
Service
ConfigMap
Ingress
```

The user should not eventually need to manually create these resources.

Forge should create the appropriate platform representation for them.

---

# Evolution 7 — Infrastructure as Code

As our environment grows, manual infrastructure configuration becomes difficult to reproduce.

Introduce Terraform.

Questions include:

* What is desired state?
* What is Terraform state?
* Why does Terraform need state?
* What is drift?
* How are resource dependencies calculated?
* What happens when configuration changes?
* What should and should not be managed by Terraform?
* How do multiple environments differ?

Helm can package reusable Kubernetes applications.

The important property becomes:

> Infrastructure should be recreatable.

Eventually we should be capable of:

```text
destroy environment
      │
      ▼
run infrastructure definitions
      │
      ▼
restore state/data where appropriate
      │
      ▼
platform becomes operational
```

---

# Evolution 8 — Data

Forge should operate realistic stateful systems.

PostgreSQL should provide room to explore:

* persistent storage,
* migrations,
* connection pooling,
* transactions,
* backups,
* restore,
* replication concepts,
* availability tradeoffs.

Redis should provide room to explore:

* caching,
* TTLs,
* cache invalidation,
* ephemeral state,
* distributed coordination concepts where appropriate.

The project should eventually answer:

> What happens if our database disappears?

Not theoretically.

Practically.

We should perform backups and restores.

---

# Evolution 9 — Messaging

Introduce NATS first because it provides a relatively approachable way to explore asynchronous systems.

Architecture:

```text
API
 │
 │ publish job
 ▼
NATS
 │
 ├────────► Worker A
 │
 └────────► Worker B
```

Now intentionally create problems.

Examples:

```text
worker crashes after processing but before acknowledgement
```

What happens?

Potential duplicate processing.

This creates a reason to learn idempotency.

Other scenarios:

```text
consumer becomes slower than producer
```

Now we learn about backpressure.

```text
dependency fails and every message retries immediately
```

Now we can create a retry storm.

Topics eventually include:

* at-most-once,
* at-least-once,
* exactly-once claims,
* acknowledgements,
* redelivery,
* idempotency,
* ordering,
* consumer groups,
* retry strategies,
* dead-letter handling,
* queue depth,
* backpressure.

Later, comparing NATS with Kafka or RabbitMQ can reveal architectural tradeoffs.

---

# Evolution 10 — Observability

Logs alone eventually become insufficient.

Introduce the three primary observability signals.

## Logs

Structured application events.

Questions:

* What happened?
* What context surrounded the event?

Possible tool:

```text
Loki
```

---

## Metrics

Aggregated measurements over time.

Examples:

```text
requests_total
request_duration_seconds
errors_total
active_connections
queue_depth
```

Possible tools:

```text
Prometheus
Grafana
```

---

## Traces

Represent a request moving across multiple system components.

Possible tools:

```text
OpenTelemetry
Tempo
```

Example:

```text
HTTP request
    │
    ▼
API span
    │
    ├── PostgreSQL query
    │
    └── NATS publish
              │
              ▼
         Worker span
              │
              ▼
         database write
```

Forge should allow us to answer:

> Where did this request spend its time?

rather than simply:

> Something is slow.

---

# Evolution 11 — Reliability Engineering

Once the system works, actively destroy parts of it.

This is essential.

A healthy personal infrastructure project should spend significant time broken.

Examples:

## Process failure

```text
kill -9 application
```

Questions:

* What notices?
* What restarts it?
* What happens to existing requests?

---

## Container failure

Delete containers.

Observe recovery.

---

## Kubernetes failure

Delete Pods.

Kill nodes if feasible.

Create bad Deployments.

Break readiness probes.

---

## Database failure

Stop PostgreSQL.

Questions:

* Do requests hang?
* Do they fail quickly?
* What timeout applies?
* Does the connection pool recover?
* Do retries worsen the incident?

---

## Network failure

Introduce:

* latency,
* packet loss where practical,
* broken DNS,
* unreachable dependency.

---

## Memory failure

Set restrictive memory limits.

Trigger OOM behavior.

Observe:

* application,
* container,
* Kubernetes,
* metrics,
* logs.

---

## Traffic spikes

Use k6.

Observe:

* latency,
* error rate,
* CPU,
* memory,
* connections,
* queue depth,
* replica count.

Reliability engineering is not simply preventing failures.

It is understanding how the system behaves when failure inevitably occurs.

---

# Evolution 12 — Autoscaling

Once metrics and resource usage are visible, scaling becomes meaningful.

Explore:

* horizontal scaling,
* vertical scaling,
* Kubernetes HPA,
* CPU-based scaling,
* application-level metrics,
* scaling lag,
* cold starts,
* database bottlenecks.

Important realization:

> Scaling the application does not necessarily scale the system.

For example:

```text
API replicas: 2 → 50
```

may simply overwhelm:

```text
PostgreSQL
```

This creates realistic architecture problems.

---

# Evolution 13 — GitOps

Eventually move toward declarative delivery.

Possible architecture:

```text
Developer
    │
    ▼
forge deploy
    │
    ▼
Forge changes desired state
    │
    ▼
Git repository
    │
    ▼
Argo CD
    │
    ▼
Kubernetes
```

This introduces concepts such as:

* desired state,
* reconciliation,
* auditability,
* declarative deployments,
* rollback through Git,
* drift detection.

Git becomes a control interface rather than merely source storage.

---

# Evolution 14 — Developer Platform

At this point, Forge begins becoming an actual platform.

Instead of developers writing Kubernetes configuration, they express intent.

Example:

```yaml
name: payments

runtime:
  type: go

http:
  port: 8080

health:
  path: /health

database:
  postgres: true

cache:
  redis: true

resources:
  cpu: 250m
  memory: 256Mi

scaling:
  min: 2
  max: 10
```

Forge may transform this into:

```text
Deployment
Service
Ingress
ConfigMap
Secret references
Database dependency
Monitoring
Autoscaling
Logging
Tracing
```

The developer works with platform concepts.

Forge handles infrastructure implementation.

That is the key transition from infrastructure engineering to platform engineering.

---

# Control Plane

A later Forge architecture may contain a control plane.

For example:

```text
forge CLI
    │
    ▼
Forge API
    │
    ├──── PostgreSQL
    │
    ├──── NATS
    │
    └──── Kubernetes API
```

The control plane could track concepts such as:

```text
Application
Environment
Deployment
Release
Service
Build
Revision
```

However, the control plane should only be introduced when local CLI-driven architecture begins demonstrating limitations.

Do not prematurely create a complex distributed platform.

---

# Desired Developer Experience

The long-term user should think in terms such as:

```text
application
environment
deployment
release
logs
metrics
scale
rollback
```

rather than:

```text
ReplicaSet
Ingress
PersistentVolumeClaim
ConfigMap
```

Those underlying systems still exist.

Forge provides an opinionated abstraction over them.

This is the same fundamental idea behind internal developer platforms.

---

# Internal Developer Platform Concepts

Forge may eventually demonstrate concepts such as:

* golden paths,
* service templates,
* standardized deployment,
* self-service environments,
* infrastructure APIs,
* policy enforcement,
* observability by default,
* standardized security,
* platform ownership.

The goal is not merely:

> Can operations deploy an application?

It becomes:

> Can a developer safely deploy and operate an application without needing to understand every infrastructure detail?

Forge should provide that experience precisely because its author does understand those details.

---

# Security Vision

Security should evolve with the platform.

Potential topics include:

```text
Unix permissions
      ↓
container permissions
      ↓
secrets management
      ↓
TLS
      ↓
Kubernetes RBAC
      ↓
service identities
      ↓
least privilege
      ↓
supply-chain security
```

We should avoid simply placing secrets directly into Git.

Eventually investigate:

* secret storage,
* secret rotation,
* credential expiration,
* certificate management,
* workload identity concepts,
* image scanning,
* dependency scanning.

Security should remain connected to realistic threats and operational requirements.

---

# Infrastructure Repository Structure

The project may eventually resemble:

```text
forge/
│
├── AGENTS.md
├── README.md
│
├── cmd/
│   └── forge/
│
├── internal/
│   ├── config/
│   ├── deploy/
│   ├── runtime/
│   ├── kubernetes/
│   └── platform/
│
├── examples/
│   ├── hello-api/
│   ├── worker/
│   └── distributed-demo/
│
├── infra/
│   ├── docker/
│   ├── kubernetes/
│   ├── terraform/
│   ├── helm/
│   ├── gitops/
│   └── observability/
│
├── docs/
│   ├── PROJECT_VISION.md
│   ├── architecture/
│   ├── decisions/
│   ├── incidents/
│   ├── runbooks/
│   └── concepts/
│
└── tests/
    ├── integration/
    ├── load/
    └── failure/
```

This is illustrative.

Do not create empty architecture for its own sake.

Directories should appear when their responsibilities actually emerge.

---

# Incident Documentation

Failure investigation is part of the project.

Meaningful incidents should be documented.

Example:

```text
docs/incidents/004-database-pool-exhaustion.md
```

Suggested structure:

```md
# Incident

## Symptoms

What did we observe?

## Impact

What stopped working?

## Timeline

What happened?

## Investigation

What signals did we inspect?

## Root Cause

Why did this actually happen?

## Resolution

What restored service?

## Improvements

How could we detect, prevent, or tolerate this next time?

## Lessons

What system behavior did this incident reveal?
```

Over time, the incident folder should become evidence of infrastructure understanding.

---

# Architectural Decision Records

Important architecture choices can be documented under:

```text
docs/decisions/
```

Example:

```text
001-use-nats-before-kafka.md
```

Possible format:

```md
# Decision

## Context

What problem are we solving?

## Options

What alternatives exist?

## Decision

What did we choose?

## Why

What tradeoffs led to this choice?

## Consequences

What do we gain and what do we give up?
```

The objective is to learn architectural reasoning.

---

# Production Thinking

Forge is personal and learning-oriented, but explanations should connect to production reality.

Every component can eventually be evaluated through dimensions such as:

```text
Correctness
Reliability
Availability
Scalability
Security
Observability
Maintainability
Cost
Operational complexity
Developer experience
```

There is rarely one universally correct architecture.

Engineering means understanding tradeoffs.

---

# What Success Looks Like

Forge is successful if, after building it, I can confidently reason through scenarios such as:

> A service is returning 503 errors after deployment. Where do I investigate?

> Kubernetes says the Pod is Running but users cannot reach the application. Why could that happen?

> PostgreSQL latency suddenly increased. How would that appear in metrics and traces?

> A NATS message was processed twice. Why is that possible and how should the application handle it?

> A worker died halfway through processing a job. What guarantees exist?

> A deployment increased CPU usage by 500%. How do we detect and rollback it?

> We need to recreate the platform on a new machine. What state must exist outside the cluster?

> We increased API replicas from 3 to 30 and performance became worse. Why?

> Terraform wants to destroy an important resource. What could have caused that?

> DNS resolution stopped working inside the cluster. How do we isolate the problem?

> Users report intermittent latency but CPU and memory look normal. What should we inspect next?

> What exactly happens between typing a URL and a Go handler receiving the HTTP request?

Being able to reason through these questions matters more than simply knowing commands.

---

# Free / Local-First Principle

Forge should remain local-first while learning.

The intended local stack may eventually include:

```text
Linux
Go
Docker / Podman
kind / k3d
Kubernetes
PostgreSQL
Redis
NATS
Terraform
Helm
Argo CD
GitHub Actions
Prometheus
Grafana
Loki
Tempo
OpenTelemetry
Alertmanager
k6
```

Most of the system should be reproducible on a developer machine.

Cloud deployment can come later.

When cloud infrastructure is introduced, it should primarily help explore:

* real networking,
* remote systems,
* IAM,
* managed infrastructure,
* public DNS,
* TLS,
* high availability,
* cost,
* persistent cloud resources,
* production-like failure domains.

Cloud providers should be an additional infrastructure layer, not the starting point.

---

# Final Vision

Forge begins with:

```text
./hello-api
```

running as a Linux process.

It grows through:

```text
Linux
    ↓
Networking
    ↓
systemd
    ↓
Docker
    ↓
CI/CD
    ↓
Kubernetes
    ↓
Terraform
    ↓
Messaging
    ↓
Observability
    ↓
Reliability
    ↓
GitOps
    ↓
Platform Engineering
```

Eventually a developer sees:

```bash
forge deploy payments
```

while Forge sees:

```text
source
  │
  ▼
build
  │
  ▼
artifact
  │
  ▼
desired state
  │
  ▼
scheduler
  │
  ▼
containers
  │
  ├── networking
  ├── configuration
  ├── secrets
  ├── storage
  ├── health checks
  ├── metrics
  ├── logs
  └── traces
```

The final abstraction is simple only because we have learned the complicated system underneath it.

That is the purpose of Forge.

**We are not learning tools so that we can operate infrastructure manually forever.**

We are learning infrastructure deeply enough that we can eventually build software that operates it for us.
