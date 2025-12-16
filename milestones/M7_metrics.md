# M7: Prometheus Metrics

**Goal:** Expose metrics endpoint for monitoring and observability.

**Time:** 2-3 hours  
**Output:** `/metrics` endpoint with Prometheus-format metrics

---

## What We're Building

```
┌─────────────┐         ┌─────────────┐
│  Prometheus │ ─scrape─│   Gateway   │
│             │         │  /metrics   │
└─────────────┘         └─────────────┘
                              │
                        Exposes:
                        • Request counts
                        • Latency histograms
                        • Circuit breaker state
```

---

## Metrics to Expose

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `llm_requests_total` | Counter | provider, status, model | Total requests by provider and status |
| `llm_request_duration_seconds` | Histogram | provider, status | Request latency distribution |
| `llm_circuit_state` | Gauge | provider | Circuit breaker state (0=closed, 1=open) |
| `llm_retry_total` | Counter | provider | Total retry attempts |
| `llm_failover_total` | Counter | from, to | Total failover events |

---

## Key Concepts

### 1. Prometheus Metrics Types

- **Counter**: Only goes up (requests, errors)
- **Gauge**: Can go up or down (circuit state, active connections)
- **Histogram**: Distribution of values (latency)

### 2. Labels

Labels allow slicing metrics:

```prometheus
# Total successful requests to OpenAI
llm_requests_total{provider="openai", status="success"} 150

# Total failed requests to Anthropic
llm_requests_total{provider="anthropic", status="error"} 5
```

### 3. prom-client Library

The standard Node.js library for Prometheus metrics.

---

## Implementation

### Install prom-client

```bash
npm install prom-client
```

### Metrics Registry

```typescript
// src/metrics/metrics.ts
import {
  Registry,
  Counter,
  Histogram,
  Gauge,
  collectDefaultMetrics,
} from 'prom-client';

// Create a custom registry
export const registry = new Registry();

// Collect default Node.js metrics (memory, CPU, etc.)
collectDefaultMetrics({ register: registry });

// Request counter
export const requestsTotal = new Counter({
  name: 'llm_requests_total',
  help: 'Total number of LLM requests',
  labelNames: ['provider', 'status', 'model'] as const,
  registers: [registry],
});

// Request duration histogram
export const requestDuration = new Histogram({
  name: 'llm_request_duration_seconds',
  help: 'LLM request duration in seconds',
  labelNames: ['provider', 'status'] as const,
  buckets: [0.1, 0.5, 1, 2, 5, 10, 30], // seconds
  registers: [registry],
});

// Circuit breaker state gauge
export const circuitState = new Gauge({
  name: 'llm_circuit_state',
  help: 'Circuit breaker state (0=closed, 1=open)',
  labelNames: ['provider'] as const,
  registers: [registry],
});

// Retry counter
export const retryTotal = new Counter({
  name: 'llm_retry_total',
  help: 'Total number of retry attempts',
  labelNames: ['provider'] as const,
  registers: [registry],
});

// Failover counter
export const failoverTotal = new Counter({
  name: 'llm_failover_total',
  help: 'Total number of failover events',
  labelNames: ['from', 'to'] as const,
  registers: [registry],
});
```

### Metrics Middleware

```typescript
// src/middleware/metrics.ts
import { Request, Response, NextFunction } from 'express';
import { requestsTotal, requestDuration } from '../metrics/metrics';

export function metricsMiddleware() {
  return (req: Request, res: Response, next: NextFunction) => {
    // Skip metrics endpoint itself
    if (req.path === '/metrics') {
      return next();
    }

    const startTime = Date.now();

    // Store original end function
    const originalEnd = res.end;

    // Override end to capture metrics
    res.end = function (this: Response, ...args: any[]) {
      const duration = (Date.now() - startTime) / 1000;
      const status = res.statusCode < 400 ? 'success' : 'error';

      // Only track API endpoints
      if (req.path.startsWith('/v1/')) {
        const model = req.body?.model || 'unknown';
        const provider = (res as any).provider || 'unknown';

        requestsTotal.inc({
          provider,
          status,
          model,
        });

        requestDuration.observe(
          { provider, status },
          duration
        );
      }

      // Call original end
      return originalEnd.apply(this, args);
    };

    next();
  };
}
```

### Metrics Endpoint

```typescript
// src/index.ts (additions)
import { registry } from './metrics/metrics';
import { metricsMiddleware } from './middleware/metrics';

// Add metrics middleware
app.use(metricsMiddleware());

// Metrics endpoint
app.get('/metrics', async (req, res) => {
  try {
    res.set('Content-Type', registry.contentType);
    res.end(await registry.metrics());
  } catch (error) {
    res.status(500).end(error instanceof Error ? error.message : 'Error');
  }
});
```

### Recording Metrics in Provider Client

```typescript
// src/provider/client.ts (additions)
import {
  requestsTotal,
  requestDuration,
  circuitState,
  retryTotal,
  failoverTotal,
} from '../metrics/metrics';

export class ProviderClient {
  // ... existing code ...

  async call(requestId: string, req: ChatRequest): Promise<ProviderResponse> {
    const startTime = Date.now();
    let currentProvider: Provider | null = this.router.getHealthyProvider();

    // Update circuit state metrics
    this.updateCircuitMetrics();

    // ... existing provider selection ...

    while (currentProvider) {
      try {
        const result = await this.callWithRetry(requestId, req, currentProvider);

        // Record success metrics
        const duration = (Date.now() - startTime) / 1000;
        requestDuration.observe(
          { provider: currentProvider.name, status: 'success' },
          duration
        );

        return result;
      } catch (error) {
        // Record failure metrics
        const duration = (Date.now() - startTime) / 1000;
        requestDuration.observe(
          { provider: currentProvider.name, status: 'error' },
          duration
        );

        if (error instanceof ProviderError && error.failover) {
          const nextProvider = this.router.getNextProvider(currentProvider.name);

          if (nextProvider) {
            // Record failover
            failoverTotal.inc({
              from: currentProvider.name,
              to: nextProvider.name,
            });
          }

          currentProvider = nextProvider;
          continue;
        }

        throw error;
      }
    }

    throw new Error('All providers failed');
  }

  private async callWithRetry(
    requestId: string,
    req: ChatRequest,
    provider: Provider
  ): Promise<ProviderResponse> {
    // ... existing retry logic ...

    for (let attempt = 1; attempt <= this.retryConfig.maxAttempts; attempt++) {
      try {
        // ... attempt logic ...
      } catch (error) {
        // Record retry metric
        if (attempt < this.retryConfig.maxAttempts) {
          retryTotal.inc({ provider: provider.name });
        }
        // ... rest of error handling ...
      }
    }
    // ...
  }

  private updateCircuitMetrics(): void {
    for (const provider of this.router.getAllProviders()) {
      circuitState.set(
        { provider: provider.name },
        provider.circuit.isOpen() ? 1 : 0
      );
    }
  }
}
```

---

## Example Metrics Output

```prometheus
# HELP llm_requests_total Total number of LLM requests
# TYPE llm_requests_total counter
llm_requests_total{provider="openai",status="success",model="gpt-4"} 150
llm_requests_total{provider="openai",status="error",model="gpt-4"} 5
llm_requests_total{provider="anthropic",status="success",model="gpt-4"} 10

# HELP llm_request_duration_seconds LLM request duration in seconds
# TYPE llm_request_duration_seconds histogram
llm_request_duration_seconds_bucket{provider="openai",status="success",le="0.1"} 10
llm_request_duration_seconds_bucket{provider="openai",status="success",le="0.5"} 80
llm_request_duration_seconds_bucket{provider="openai",status="success",le="1"} 140
llm_request_duration_seconds_bucket{provider="openai",status="success",le="2"} 148
llm_request_duration_seconds_bucket{provider="openai",status="success",le="5"} 150
llm_request_duration_seconds_bucket{provider="openai",status="success",le="10"} 150
llm_request_duration_seconds_bucket{provider="openai",status="success",le="30"} 150
llm_request_duration_seconds_bucket{provider="openai",status="success",le="+Inf"} 150
llm_request_duration_seconds_sum{provider="openai",status="success"} 75.5
llm_request_duration_seconds_count{provider="openai",status="success"} 150

# HELP llm_circuit_state Circuit breaker state (0=closed, 1=open)
# TYPE llm_circuit_state gauge
llm_circuit_state{provider="openai"} 0
llm_circuit_state{provider="anthropic"} 1

# HELP llm_retry_total Total number of retry attempts
# TYPE llm_retry_total counter
llm_retry_total{provider="openai"} 25

# HELP llm_failover_total Total number of failover events
# TYPE llm_failover_total counter
llm_failover_total{from="openai",to="anthropic"} 3
```

---

## Testing

### Test 1: Check Metrics Endpoint

```bash
curl http://localhost:8080/metrics

# Expected: Prometheus-format metrics output
```

### Test 2: Verify Request Counter

```bash
# Make some requests
for i in {1..5}; do
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
done

# Check metrics
curl http://localhost:8080/metrics | grep llm_requests_total

# Expected: llm_requests_total{...} 5
```

### Test 3: Verify Latency Histogram

```bash
curl http://localhost:8080/metrics | grep llm_request_duration

# Expected: Histogram buckets with counts
```

### Test 4: Verify Circuit State

```bash
# Trigger circuit breaker (make requests fail)
# Then check metrics
curl http://localhost:8080/metrics | grep llm_circuit_state

# Expected: llm_circuit_state{provider="openai"} 1
```

### Test 5: Verify Failover Counter

```bash
# Trigger failover scenario (429 on primary)
curl http://localhost:8080/metrics | grep llm_failover_total

# Expected: llm_failover_total{from="openai",to="anthropic"} 1
```

---

## File Structure

```
src/
  metrics/
    metrics.ts         # Metric definitions
  middleware/
    metrics.ts         # Metrics middleware
```

---

## Grafana Dashboard (Optional)

Example PromQL queries for a dashboard:

```promql
# Request rate
rate(llm_requests_total[5m])

# Error rate
rate(llm_requests_total{status="error"}[5m]) / rate(llm_requests_total[5m])

# P99 latency
histogram_quantile(0.99, rate(llm_request_duration_seconds_bucket[5m]))

# Circuit breaker status
llm_circuit_state

# Failover rate
rate(llm_failover_total[5m])
```

---

## Definition of Done

- [ ] `/metrics` endpoint returns Prometheus format
- [ ] `llm_requests_total` counter with provider, status, model labels
- [ ] `llm_request_duration_seconds` histogram with buckets
- [ ] `llm_circuit_state` gauge per provider
- [ ] `llm_retry_total` counter per provider
- [ ] `llm_failover_total` counter with from/to labels
- [ ] Default Node.js metrics included
- [ ] Metrics update correctly during request lifecycle
- [ ] Circuit state reflects actual breaker status

