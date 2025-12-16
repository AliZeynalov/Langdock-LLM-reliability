# M8: Docker + Polish

**Goal:** Package everything in Docker for easy deployment and demo.

**Time:** 2-3 hours  
**Output:** `docker-compose up` starts the entire system

---

## What We're Building

```
docker-compose up
       │
       ├── Gateway (:8080)
       │      ├── /v1/chat/completions
       │      ├── /metrics
       │      └── /health
       │
       ├── Mock OpenAI (:8001)
       │      └── /v1/chat/completions
       │
       └── Mock Anthropic (:8002)
              └── /v1/chat/completions
```

---

## File Structure

```
.
├── src/
│   ├── index.ts
│   ├── mock-provider.ts
│   └── ...
├── Dockerfile
├── Dockerfile.mock
├── docker-compose.yml
├── demo.sh
├── package.json
├── tsconfig.json
└── README.md
```

---

## Implementation

### Dockerfile (Gateway)

```dockerfile
# Dockerfile
FROM node:20-alpine AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./
COPY tsconfig.json ./

# Install dependencies
RUN npm ci

# Copy source
COPY src/ ./src/

# Build TypeScript
RUN npm run build

# Production stage
FROM node:20-alpine

WORKDIR /app

# Copy package files and install production deps only
COPY package*.json ./
RUN npm ci --only=production

# Copy built files
COPY --from=builder /app/dist ./dist

# Set environment
ENV NODE_ENV=production
ENV PORT=8080

EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["node", "dist/index.js"]
```

### Dockerfile.mock (Mock Provider)

```dockerfile
# Dockerfile.mock
FROM node:20-alpine AS builder

WORKDIR /app

COPY package*.json ./
COPY tsconfig.json ./

RUN npm ci

COPY src/ ./src/

RUN npm run build

FROM node:20-alpine

WORKDIR /app

COPY package*.json ./
RUN npm ci --only=production

COPY --from=builder /app/dist ./dist

ENV NODE_ENV=production
ENV PORT=8001

EXPOSE 8001

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT}/health || exit 1

CMD ["node", "dist/mock-provider.js"]
```

### docker-compose.yml

```yaml
# docker-compose.yml
version: '3.8'

services:
  gateway:
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
    environment:
      - NODE_ENV=production
      - PORT=8080
      - OPENAI_URL=http://mock-openai:8001
      - ANTHROPIC_URL=http://mock-anthropic:8002
      - LOG_LEVEL=info
    depends_on:
      mock-openai:
        condition: service_healthy
      mock-anthropic:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 10s

  mock-openai:
    build:
      context: .
      dockerfile: Dockerfile.mock
    ports:
      - "8001:8001"
    environment:
      - PORT=8001
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8001/health"]
      interval: 10s
      timeout: 5s
      retries: 3

  mock-anthropic:
    build:
      context: .
      dockerfile: Dockerfile.mock
    ports:
      - "8002:8002"
    environment:
      - PORT=8002
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8002/health"]
      interval: 10s
      timeout: 5s
      retries: 3
```

### Package.json Scripts

```json
{
  "name": "llm-reliability-gateway",
  "version": "1.0.0",
  "main": "dist/index.js",
  "scripts": {
    "build": "tsc",
    "start": "node dist/index.js",
    "dev": "ts-node src/index.ts | pino-pretty",
    "mock": "ts-node src/mock-provider.ts",
    "docker:build": "docker-compose build",
    "docker:up": "docker-compose up",
    "docker:down": "docker-compose down",
    "demo": "./demo.sh"
  },
  "dependencies": {
    "express": "^4.18.2",
    "pino": "^8.16.0",
    "prom-client": "^15.0.0",
    "zod": "^3.22.4"
  },
  "devDependencies": {
    "@types/express": "^4.17.20",
    "@types/node": "^20.9.0",
    "pino-pretty": "^10.2.3",
    "ts-node": "^10.9.1",
    "typescript": "^5.2.2"
  }
}
```

### tsconfig.json

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "commonjs",
    "lib": ["ES2022"],
    "outDir": "./dist",
    "rootDir": "./src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true,
    "resolveJsonModule": true,
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true
  },
  "include": ["src/**/*"],
  "exclude": ["node_modules", "dist"]
}
```

### Demo Script

```bash
#!/bin/bash
# demo.sh - Run all demo scenarios

set -e

BASE_URL="${BASE_URL:-http://localhost:8080}"
MOCK_OPENAI="${MOCK_OPENAI:-http://localhost:8001}"

echo "================================"
echo "LLM Reliability Gateway Demo"
echo "================================"
echo ""

# Helper function
request() {
  local name=$1
  local url=$2
  local data=$3
  
  echo "--- $name ---"
  echo "Request: $data"
  echo ""
  curl -s -X POST "$url/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -d "$data" | jq .
  echo ""
  echo ""
}

# Scenario 1: Happy Path
echo "=== Scenario 1: Happy Path ==="
request "Normal Request" "$BASE_URL" \
  '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'

sleep 1

# Scenario 2: Validation Error
echo "=== Scenario 2: Validation Error ==="
request "Missing Model" "$BASE_URL" \
  '{"messages": [{"role": "user", "content": "Hello"}]}'

sleep 1

# Scenario 3: Rate Limit + Failover
echo "=== Scenario 3: Rate Limit (429) ==="
echo "Configuring mock to return 429..."
request "Rate Limited Request" "$MOCK_OPENAI" \
  '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}' &>/dev/null || true

# Make request through gateway with ?fail=429 on mock
echo "Request through gateway (should failover to Anthropic)..."
curl -s -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}' | jq .
echo ""

sleep 1

# Scenario 4: Server Error + Retry
echo "=== Scenario 4: Server Error (500) ==="
echo "This will show retry behavior in logs..."
curl -s -X POST "$MOCK_OPENAI/v1/chat/completions?fail=500" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}' || true
echo ""

sleep 1

# Scenario 5: Streaming
echo "=== Scenario 5: Streaming ==="
echo "Streaming response:"
curl -s -N -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}], "stream": true}'
echo ""
echo ""

sleep 1

# Scenario 6: Metrics
echo "=== Scenario 6: Metrics ==="
echo "Fetching metrics..."
curl -s "$BASE_URL/metrics" | grep -E "^llm_" | head -20
echo ""

echo "================================"
echo "Demo Complete!"
echo "================================"
```

### README.md

```markdown
# LLM Reliability Gateway

A reliability layer for LLM APIs with retry, circuit breaker, and failover capabilities.

## Features

- **Retry with Exponential Backoff**: Automatic retries for transient failures
- **Circuit Breaker**: Skip broken providers automatically
- **Failover**: Route to backup provider on failures
- **Request Validation**: Fast-fail invalid requests
- **Streaming Support**: SSE streaming with error handling
- **Prometheus Metrics**: Full observability

## Quick Start

### Using Docker (Recommended)

```bash
# Build and start all services
docker-compose up --build

# In another terminal, run demo
./demo.sh
```

### Manual Setup

```bash
# Install dependencies
npm install

# Terminal 1: Start mock OpenAI
PORT=8001 npm run mock

# Terminal 2: Start mock Anthropic
PORT=8002 npm run mock

# Terminal 3: Start gateway
npm run dev
```

## API Endpoints

### Chat Completions

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

### Streaming

```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```

### Health Check

```bash
curl http://localhost:8080/health
```

### Metrics

```bash
curl http://localhost:8080/metrics
```

## Mock Provider

The mock provider supports failure simulation:

| Parameter | Example | Description |
|-----------|---------|-------------|
| `delay` | `?delay=3000` | Add latency (ms) |
| `fail` | `?fail=500` | Return error code |
| `stream` | `?stream=true` | Enable streaming |
| `fail_chunk` | `?fail_chunk=3` | Fail at chunk N |

## Architecture

```
Client → Gateway → Circuit Breaker → Provider
              ↓           ↓
           Retry      Failover
              ↓           ↓
          Metrics    Logging
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `PORT` | `8080` | Gateway port |
| `OPENAI_URL` | `http://localhost:8001` | Primary provider URL |
| `ANTHROPIC_URL` | `http://localhost:8002` | Backup provider URL |
| `LOG_LEVEL` | `info` | Logging level |

## License

MIT
```

---

## Testing

### Test 1: Build Docker Images

```bash
docker-compose build

# Expected: All images build successfully
```

### Test 2: Start Services

```bash
docker-compose up

# Expected: All services start and become healthy
```

### Test 3: Health Checks

```bash
# Gateway
curl http://localhost:8080/health

# Mock OpenAI
curl http://localhost:8001/health

# Mock Anthropic
curl http://localhost:8002/health

# Expected: {"status": "healthy"} from all
```

### Test 4: Run Demo Script

```bash
./demo.sh

# Expected: All scenarios complete successfully
```

### Test 5: Clean Up

```bash
docker-compose down

# Expected: All containers stop and remove
```

---

## Development vs Production

### Development

```bash
# Run with hot reload and pretty logs
npm run dev
```

### Production (Docker)

```bash
# Build and run
docker-compose up --build -d

# View logs
docker-compose logs -f gateway

# Stop
docker-compose down
```

---

## Definition of Done

- [ ] Dockerfile builds gateway successfully
- [ ] Dockerfile.mock builds mock provider
- [ ] docker-compose.yml defines all services
- [ ] Health checks work in Docker
- [ ] Services can communicate via Docker network
- [ ] demo.sh runs all scenarios
- [ ] README.md documents usage
- [ ] `docker-compose up` starts everything
- [ ] Logs are visible in Docker
- [ ] Graceful shutdown works

