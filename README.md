# Go Microservices E-Commerce Platform

A microservices-based e-commerce backend built with **Go**, demonstrating a real-world cloud-native architecture: independent services, multiple databases, asynchronous event-driven messaging, JWT authentication, and full containerization with Docker Compose and Kubernetes.

## Architecture

![Architecture](docs/images/architecture.png)

Synchronous HTTP calls handle reads and order creation; an asynchronous **fanout**
exchange on RabbitMQ broadcasts `OrderCreated` events to the payment and shipping
consumers. Order and Cart are protected by stateless JWT validation against Keycloak;
Catalog is public.

## Services

| Service            | Port | Storage / Broker | Auth | Description |
|--------------------|------|------------------|------|-------------|
| **catalog-service**  | 8080 | MongoDB          | —    | Product catalog CRUD (`/products`) |
| **order-service**    | 8081 | PostgreSQL       | JWT  | Creates orders, validates products against catalog, publishes `OrderCreated` events |
| **cart-service**     | 8082 | Redis            | JWT  | Per-user shopping cart with 30-minute TTL (`/cart`) |
| **payment-service**  | —    | RabbitMQ (consumer) | — | Consumes order events from `payment_queue` and simulates payment processing |
| **shipping-service** | —    | RabbitMQ (consumer) | — | Consumes order events from `shipping_queue` and simulates shipping |

**Event flow:** `order-service` publishes to a RabbitMQ **fanout** exchange (`order_events`); both `payment-service` and `shipping-service` bind their own queues and process each order independently and asynchronously.

## Tech Stack

- **Language:** Go 1.26
- **Databases:** MongoDB (catalog), PostgreSQL (orders), Redis (cart)
- **Messaging:** RabbitMQ (AMQP, fanout exchange)
- **Auth:** Keycloak — stateless JWT validation via JWKS (`golang-jwt`, `MicahParks/keyfunc`)
- **Containerization:** Docker (multi-stage builds), Docker Compose
- **Orchestration:** Kubernetes (Deployments, StatefulSets, Services, Ingress, Secrets, ConfigMap)

## API Endpoints

### Catalog (`:8080`) — public
| Method | Path | Description |
|--------|------|-------------|
| GET    | `/products/`       | List all products |
| GET    | `/products/{id}/`  | Get a product by ID |
| POST   | `/products/`       | Create a product |

### Order (`:8081`) — requires `Authorization: Bearer <token>`
| Method | Path | Description |
|--------|------|-------------|
| POST   | `/orders/` | Create an order (validates items, persists, publishes event) |

### Cart (`:8082`) — requires `Authorization: Bearer <token>`
| Method | Path | Description |
|--------|------|-------------|
| GET    | `/cart/` | Get the current user's cart |
| POST   | `/cart/` | Add a product to the cart |
| DELETE | `/cart/` | Remove a product from the cart |

## Getting Started (Docker Compose)

### 1. Create the environment files

These hold secrets and are **not** committed. Copy the examples and fill in your values:

```bash
cp postgre/.env.example postgre/.env
```

Create `RabbitMQ/.env`:

```env
URL=amqp://guest:guest@rabbitmq:5672/
```

> `order-service`, `payment-service`, and `shipping-service` also expect `KEYCLOAK_JWKS_URL`
> (and `order-service` expects `DATABASE_URL`). With Compose these come from the env files / Keycloak setup.

### 2. Start the stack

```bash
docker compose up --build
```

This brings up all services plus MongoDB, PostgreSQL, Redis, and RabbitMQ
(management UI at http://localhost:15672).

### 3. Try it out

```bash
# Create a product (public)
curl -X POST http://localhost:8080/products/ \
  -H "Content-Type: application/json" \
  -d '{"name":"Keyboard","description":"Mechanical","price":49.99,"stock":10,"category":"peripherals"}'

# List products
curl http://localhost:8080/products/

# Create an order (needs a valid Keycloak JWT)
curl -X POST http://localhost:8081/orders/ \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"items":[{"productId":"<id>","quantity":2}]}'
```

## Kubernetes Deployment

Manifests live in [`k8s/`](k8s/). Secrets are intentionally **not** committed — create them
from the provided examples before applying:

```bash
# Create secrets from the examples (fill in base64-encoded values first)
cp k8s/postgresql-secret.example.yaml k8s/postgresql-secret.yaml
# ...also create k8s/keycloak-secret.yaml and k8s/rabbitmq-secret.yaml

kubectl apply -f k8s/
```

Included resources: Deployments/StatefulSets for every service and datastore, a Keycloak
deployment + ConfigMap (JWKS URL), and an Ingress routing `/products`, `/orders`, and `/cart`.

## Authentication

Protected services validate JWTs **statelessly**: on startup they fetch Keycloak's JWKS
(public keys) from `KEYCLOAK_JWKS_URL` and verify each request's `Bearer` token signature
locally — no per-request call to the auth server. The user identity is taken from the
token's `sub` claim and used to scope carts and orders.

### Security note: fixing an IDOR vulnerability

Before Keycloak was integrated, the user identity came from client-supplied input rather
than a verified source. This exposed an **IDOR (Insecure Direct Object Reference)**
vulnerability — a caller could simply pass another user's ID and read or modify carts and
orders that were not theirs.

Deriving the identity from the **verified `sub` claim** of the JWT closes this gap: the
user ID can no longer be forged or substituted by the client, so every user is restricted
to their own resources.

## Demo

**All services and datastores running on Kubernetes** (with replicas and StatefulSets):

![Pods running](docs/images/k8s-pods-running.png)

**End-to-end auth flow** — `401` with no token, `401` with an invalid token, then
successful cart/order operations with a valid JWT, and the `payment-service` consuming the
resulting `OrderCreated` event:

![Auth flow](docs/images/auth-flow.png)

## Project Structure

```
.
├── catalog-service/    # MongoDB-backed product catalog
├── order-service/      # PostgreSQL-backed orders + RabbitMQ producer
├── cart-service/       # Redis-backed cart + JWT middleware
├── payment-service/    # RabbitMQ consumer (payment_queue)
├── shipping-service/   # RabbitMQ consumer (shipping_queue)
├── k8s/                # Kubernetes manifests
├── postgre/            # PostgreSQL env example
├── RabbitMQ/           # RabbitMQ env
└── docker-compose.yml
```

## What I Learned

- **Event-driven decoupling.** Using a RabbitMQ fanout exchange lets `order-service` respond
  immediately and stay completely unaware of payment/shipping. Adding a new consumer needs
  zero changes to the producer — the practical payoff of loose coupling.
- **Stateless authentication.** Validating JWTs locally against Keycloak's JWKS (instead of
  calling the auth server on every request) keeps each service independent and scalable, and
  removes a single point of failure on the hot path.
- **Security through verified identity.** Deriving the user from the token's `sub` claim
  rather than client input is what actually closed the IDOR vulnerability — trusting client
  input is the root cause of a whole class of access-control bugs.
- **Choosing storage per service.** Mapping each service to the right datastore — MongoDB
  (flexible product documents), PostgreSQL (transactional orders), Redis (ephemeral carts
  with a TTL) — instead of forcing one database everywhere.
- **Stateful vs. stateless on Kubernetes.** Databases and brokers need StatefulSets with
  persistent volumes, while the stateless Go services run as horizontally scalable
  Deployments — and why that distinction matters.
- **Containerization & orchestration.** Multi-stage Docker builds for small images, and
  wiring the whole system together with Docker Compose locally and Kubernetes manifests
  (Deployments, StatefulSets, Services, Ingress, Secrets, ConfigMap).

