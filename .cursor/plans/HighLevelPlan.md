<!-- d0055060-46a5-4eb1-83cb-955101b770f3 627ac4a4-159d-454a-8764-7a6a3ead4e72 -->
# LLM Reliability Gateway - Implementation Plan

## Architecture

```mermaid
flowchart LR
    Client[curl] --> Gateway[Gateway :8080]
    Gateway --> Router[Router]
    Router --> CB[Circuit Breaker]
    CB --> MockA[Mock OpenAI :8001]
    CB --> MockB[Mock Anthropic :8002]
    Gateway --> Metrics[/metrics]
```

## Milestones (Build Brick by Brick)

Each milestone is ~3-4 hours and produces something **testable with curl**.

### M1: Mock Provider

**Deliverable:** Fake LLM with configurable failures

- HTTP server on :8001
- `POST /v1/chat/completions` returns fake response
- Query params: `?delay=3000`, `?fail=429`, `?fail=500`
- Streaming: `?stream=true`, `?fail_chunk=3`

**Test:** `curl -X POST localhost:8001/v1/chat/completions`

---

### M2: Gateway + Simple Client

**Deliverable:** Gateway forwards requests to mock provider

- Gin server on :8080
- Request ID middleware
- Provider client with timeout
- Structured logging

**Test:** Request through gateway → mock → response

---

### M3: Retry with Backoff

**Deliverable:** Failed requests retry automatically

- Exponential backoff: 1s → 2s → 4s (max 3 attempts)
- **Retry same provider:** timeout, 5xx (temporary issues)
- **Immediate failover:** 429 rate limit (try backup provider)
- **Fail fast:** other 4xx (request is broken)

**Test:** `?delay=5000` → retry logs → success

---

### M4: Circuit Breaker + Failover

**Deliverable:** Broken provider skipped, failover works

- States: CLOSED / OPEN (simplified)
- Opens after 3 failures, auto-closes after 30s
- Router: primary → failover to secondary

**Test:** Multiple 500s → circuit opens → request to provider B

---

### M5: Request Validation

**Deliverable:** Invalid requests rejected fast

- Required: model, messages
- Range: temperature 0-2

**Test:** Missing messages → 400 in <10ms

---

### M6: Streaming Support

**Deliverable:** SSE streaming with failure handling

- Forward chunks as they arrive
- Stall: 10s timeout per chunk
- Disconnect: return partial + error
- Malformed: terminate + partial

**Test:** `?stream=true&fail_chunk=3` → partial response

---

### M7: Prometheus Metrics

**Deliverable:** `/metrics` endpoint

- `llm_requests_total{provider, status}`
- `llm_request_duration_seconds`
- `llm_circuit_state{provider}`

**Test:** Run scenarios → check metrics

---

### M8: Docker + Polish

**Deliverable:** One command setup

- Dockerfile + docker-compose.yml
- Demo script (demo.sh)
- README

**Test:** `docker-compose up` → run all scenarios

---

## File Structure

```
cmd/server/main.go
cmd/mock-provider/main.go
internal/
  models/request.go         # Done
  validator/validator.go
  provider/client.go
  provider/circuit_breaker.go
  provider/router.go
  gateway/handler.go
  gateway/middleware.go
  metrics/metrics.go
Dockerfile, docker-compose.yml, demo.sh
```

## Demo Scenarios

| # | Scenario | Mock Config |

|---|----------|-------------|

| 1 | Happy Path | Normal |

| 2 | Timeout + Retry | `?delay=5000` |

| 3 | Rate Limit + Failover | `?fail=429` |

| 4 | Circuit Breaker | Multiple 500s |

| 5 | Validation | Missing messages |

| 6 | Stream Failure | `?stream=true&fail_chunk=3` |

## Timeline (Until Monday 5pm)

- **Tue evening:** M1 (Mock Provider)
- **Wed:** M2 + M3 (Gateway + Retry)
- **Thu:** M4 (Circuit Breaker)
- **Fri:** M5 + M6 (Validation + Streaming)
- **Sun:** M7 + M8 (Metrics + Docker)
- **Mon:** Final testing + demo prep

### To-dos

- [ ] Build mock provider with configurable failures and streaming
- [ ] Create gateway server with provider client and retry logic
- [ ] Implement circuit breaker and provider failover router
- [ ] Add request validation (pre-flight checks)
- [ ] Implement streaming response handling with failure detection
- [ ] Create demo script, test scenarios, write README