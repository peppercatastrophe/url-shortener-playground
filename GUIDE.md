# URL Shortener — Guide

This guide uses Simplified Technical English (ASD-STE100).

## 1. Purpose

This guide tells you how to build a URL shortener service.
The service helps you learn five backend skills:
Redis, message queues, Kubernetes, load testing, and performance work.

You build one project.
You add one skill per phase.
This is faster than five separate projects.

## 2. What the service does

A client sends a long URL to the service.
The service makes a short code and stores the code with the long URL in PostgreSQL.
The service returns a short URL to the client.

When a person opens the short URL, the service reads the long URL from PostgreSQL.
The service sends the person to the long URL.

## 3. The data flow

1. A client sends a POST request to `/shorten` with a long URL.
2. The service makes a short code.
3. The service stores the code and the long URL in PostgreSQL.
4. The service returns the short code.
5. A client sends a GET request to `/<code>`.
6. The service reads the long URL from PostgreSQL.
7. The service returns the long URL to the client.

## 4. The skills you get

| Phase | Skill | What you learn |
|-------|-------|----------------|
| 1 | Go API, PostgreSQL | You build a RESTful API with Go Fiber. |
| 2 | Redis, caching | You add a read-through cache to the service. |
| 3 | RabbitMQ, queues | You move click writes to a worker. |
| 4 | Kubernetes | You run the API and the worker as containers. |
| 5 | Load testing, performance | You test the service under load. |
| 6 | CI/CD | You build and deploy the service from GitHub Actions. |

## 5. Before you start

You need these tools on your computer:

- Go 1.22 or newer.
- Docker, Podman, or Docker Desktop.
- `git` to clone the project.
- `curl` to test the API.

You need this to be true:

- You can run `go build ./...` with no error.
- You can start a PostgreSQL container.
- You can send HTTP requests with `curl`.

## 6. Start the project

### 6.1 Get the files

The files are in the `url-shortener` folder.
The folder has these files:

```
url-shortener/
  go.mod
  main.go
  sql/schema.sql
  docker-compose.yml
  .env.example
  GUIDE.md
```

### 6.2 Make your environment file

Copy the example file to `.env`:

```sh
cp .env.example .env
```

The `.env` file tells the service how to connect to PostgreSQL.
You do not have to change the values for local work.

## 7. Phase 1 — the API and PostgreSQL

### 7.1 Goal

You build a working API.
The API stores a long URL and reads it back.

### 7.2 Start PostgreSQL

Start the database with Docker Compose:

```sh
docker compose up -d
```

If you use Podman, use this command:

```sh
podman compose up -d
```

Wait until PostgreSQL is ready.
You can check the status with this command:

```sh
docker compose ps
```

### 7.3 Get the Go dependencies

Go must download the libraries that the service uses.
Run this command:

```sh
go mod tidy
```

### 7.4 Start the service

Start the service with this command:

```sh
go run .
```

The service prints `listening on :3000` when it is ready.

### 7.5 Test the service

Open a second terminal.
Make a short URL with this command:

```sh
curl -X POST localhost:3000/shorten \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/long/path"}'
```

The service returns a response like this:

```json
{"code":"8TawxZ","url":"https://example.com/long/path"}
```

Use the `code` value in the next command.
Open the short URL with this command:

```sh
curl -o /dev/null -w "%{http_code} -> %{redirect_url}\n" localhost:3000/8TawxZ
```

The service returns a `301` status and the long URL.

### 7.6 Acceptance for Phase 1

- A POST to `/shorten` returns a short code.
- A GET to `/<code>` returns the long URL with status `301`.
- A GET to an unknown code returns status `404`.
- The data stays in PostgreSQL after you stop and start the service.

## 8. Phase 2 — add Redis

### 8.1 Goal

You add a Redis cache to the service.
The cache keeps the most-used URLs in memory.
This makes reads faster and lowers the load on PostgreSQL.

### 8.2 Add Redis to Docker Compose

Add a new service to `docker-compose.yml`:

```yaml
  redis:
    image: redis:7-alpine
    container_name: shortener-redis
    ports:
      - "6379:6379"
```

Start Redis with this command:

```sh
docker compose up -d
```

### 8.3 Add the Go Redis library

Get the library with this command:

```sh
go get github.com/redis/go-redis/v9
```

### 8.4 Change the Redirect handler

Change the work in the `Redirect` handler:

1. Try to read the URL from Redis first.
2. If Redis has the URL, return the URL. This is a cache HIT.
3. If Redis does not have the URL, read the URL from PostgreSQL. This is a cache MISS.
4. Write the URL to Redis with a time limit, for example 60 seconds.
5. Return the URL to the client.

Use the key format `url:<code>` in Redis.

### 8.5 Acceptance for Phase 2

- The first read of a code goes to PostgreSQL, then writes to Redis.
- The second read of the same code goes to Redis.
- If you delete the row from PostgreSQL, the URL still works until the cache key expires.
- The service does not fail if Redis is not available. It falls back to PostgreSQL.

Note for your CV: write how much faster the read became.
For example: "Read latency went from 1.6 ms to 0.2 ms."

## 9. Phase 3 — add RabbitMQ

### 9.1 Goal

You move the click records out of the read path.
Each time a person opens a short URL, the service sends a message to RabbitMQ.
A worker reads the messages and writes the clicks to PostgreSQL.

This keeps the write load away from the read path.

### 9.2 Add RabbitMQ to Docker Compose

Add this service:

```yaml
  rabbitmq:
    image: rabbitmq:3-management-alpine
    container_name: shortener-rabbit
    ports:
      - "5672:5672"
      - "15672:15672"
```

The port `15672` opens the RabbitMQ web UI.
Open `http://localhost:15672` in a browser.
Log in with the user `guest` and the password `guest`.

### 9.3 Add the Go RabbitMQ library

Get the library with this command:

```sh
go get github.com/rabbitmq/amqp091-go
```

### 9.4 Make a click event

Add a new table for clicks to `sql/schema.sql`:

```sql
CREATE TABLE IF NOT EXISTS clicks (
    id          BIGSERIAL PRIMARY KEY,
    code        VARCHAR(16) NOT NULL,
    clicked_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 9.5 Change the Redirect handler

In the `Redirect` handler, after the redirect, do this:

1. Make a message that has the code and the current time.
2. Send the message to a queue named `clicks`.
3. Do not wait for the worker. Return the redirect to the client.

### 9.6 Make a worker

Make a new program, for example `cmd/worker/main.go`.
The worker does this work:

1. Connect to RabbitMQ.
2. Listen on the `clicks` queue.
3. For each message, insert a row into the `clicks` table.
4. Tell RabbitMQ that the message is done.

### 9.7 Acceptance for Phase 3

- A GET to `/<code>` returns the redirect without delay.
- A row appears in the `clicks` table after the request.
- If the worker is stopped, the API keeps working. Messages stay in the queue.
- When you start the worker again, it processes the messages that stayed in the queue.

## 10. Phase 4 — add Kubernetes

### 10.1 Goal

You run the API and the worker as containers in Kubernetes.
You use `kind`, which runs a small Kubernetes cluster on your computer.

### 10.2 Install kind

Install `kind` from the kind website: `https://kind.sigs.k8s.io`.

Make a cluster with this command:

```sh
kind create cluster --name shortener
```

### 10.3 Make container images

Make a `Dockerfile` for the API:

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 go build -o /out/app .

FROM alpine:3.20
COPY --from=build /out/app /app
EXPOSE 3000
ENTRYPOINT ["/app"]
```

Build the image and load it into kind:

```sh
docker build -t shortener-api:dev .
kind load docker-image shortener-api:dev --name shortener
```

### 10.4 Make Kubernetes files

Make a folder named `k8s`.
Put these files in the folder:

- `api-deployment.yaml` — runs 2 copies of the API.
- `api-service.yaml` — sends traffic to the API pods.
- `worker-deployment.yaml` — runs 1 copy of the worker.
- `configmap.yaml` — holds the `DATABASE_URL` and the Redis address.
- `secret.yaml` — holds the database password.

A Deployment tells Kubernetes how many copies to run.
A Service gives the API one address that stays the same.
A ConfigMap holds values that are not secret.
A Secret holds values that are secret.

### 10.5 Apply the files

Apply all files with this command:

```sh
kubectl apply -f k8s/
```

### 10.6 Send traffic to the API

Tell Kubernetes to send port 3000 to your computer:

```sh
kubectl port-forward svc/api 3000:3000
```

Now you can send requests to `localhost:3000` as in Phase 1.

### 10.7 Acceptance for Phase 4

- `kubectl get pods` shows the API pods and the worker pod as `Running`.
- `kubectl scale deployment api --replicas=4` makes 4 copies of the API.
- If you delete one pod, Kubernetes makes a new one.
- The `clicks` table gets rows even when the API and the worker run in different pods.

## 11. Phase 5 — load test and performance

### 11.1 Goal

You test the service under load.
You find the limit of the service and you make the limit higher.

### 11.2 Install k6

Install `k6` from the k6 website: `https://k6.io`.

### 11.3 Make a test

Make a file named `load.js`:

```javascript
import http from 'k6/http';

export const options = {
  stages: [
    { duration: '30s', target: 500 },
    { duration: '1m', target: 1000 },
    { duration: '30s', target: 0 },
  ],
};

export default function () {
  http.get('http://localhost:3000/<code>');
}
```

Replace `<code>` with a code that exists in your database.

### 11.4 Run the test

Run the test with this command:

```sh
k6 run load.js
```

### 11.5 Read the result

k6 prints these values:

- `http_req_duration` — the time for one request.
- `http_req_failed` — the part of requests that failed.
- `iterations` — the total number of requests.

### 11.6 Compare two states

Run the test two times:

1. Run the test with Redis OFF.
2. Run the test with Redis ON.

Write the difference.
For example: "With Redis, the p99 latency went from 12 ms to 2 ms at 1000 RPS."

### 11.7 Acceptance for Phase 5

- You can run a load test with k6.
- You have a written number for RPS and for p99 latency.
- You have a written comparison between two states.

## 12. Phase 6 — CI/CD

### 12.1 Goal

Each time you push to the `main` branch on GitHub, a pipeline builds the image and deploys it.

### 12.2 Make a workflow file

Make a file named `.github/workflows/deploy.yml`:

```yaml
name: build-and-deploy
on:
  push:
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: go test ./...
      - run: go build ./...
```

### 12.3 Add the deploy step

Add steps that do this work:

1. Build a Docker image.
2. Push the image to GitHub Container Registry.
3. Apply the Kubernetes files to a cluster.

For a local test, you can use a `kind` cluster inside the GitHub Actions run.

### 12.4 Acceptance for Phase 6

- A push to `main` starts the pipeline.
- The pipeline runs `go test` and `go build`.
- The pipeline builds a Docker image.
- The pipeline shows a green check when all steps pass.

## 13. Resources

- Redis docs: `https://redis.io/docs`
- RabbitMQ Go tutorial: `https://www.rabbitmq.com/getstarted.html`
- kind docs: `https://kind.sigs.k8s.io`
- Kubernetes concepts (Deployment, Service, ConfigMap, Secret): `https://kubernetes.io/docs/concepts`
- k6 docs: `https://k6.io/docs`
- Go Fiber docs: `https://docs.gofiber.io`

## 14. Order of work

Do the phases in order.
Do not start Phase 2 before Phase 1 works.
Each phase builds on the phase before it.

When a phase passes its acceptance test, do the next phase.
