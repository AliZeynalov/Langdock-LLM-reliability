# LangDock LLM Reliability System - Implementation Guide

**Goal:** Demo-ready MVP showing retry, failover, and circuit breaker
**Time Budget:** ~18-20 hours
**Approach:** Build only what's needed for 5 demo scenarios

---

## What You'll Demo

A **reliability orchestrator** that makes LLM API calls bulletproof.

### 5 Live Scenarios (10 minutes)

1. **Happy Path** (30s) - Request succeeds first try
2. **Timeout + Retry** (1-2min) - Automatic retry with exponential backoff
3. **Rate Limit + Failover** (1min) - Switch from OpenAI → Anthropic instantly
4. **Circuit Breaker** (2min) - After 5 failures, skip broken provider
5. **Validation** (30s) - Invalid request fails in <10ms, no API call

**The "Aha" Moment**: Without this system, one timeout crashes your app. With it, the system retries → tries another provider → succeeds. App never knows there was a problem.

---

## Architecture Overview

```
Client Request
    ↓
[Validator] ← Fail fast on invalid input
    ↓
[Orchestrator] ← Retry loop with backoff
    ↓
[Router] ← Select provider (openai/anthropic)
    ↓
[Circuit Breaker] ← Skip if provider is down
    ↓
[HTTP Client] ← Make actual API call
    ↓
LLM Provider (OpenAI/Anthropic)
```

**Request ID tracks the entire journey through all retries.**

---

## Core Components

### 1. Data Models (`internal/models/`)

**Request:**
```go
type Request struct {
    Model            string    `json:"model"`              // "gpt-4"
    Messages         []Message `json:"messages"`
    Temperature      float64   `json:"temperature,omitempty"`
    PreferredProvider string   `json:"preferred_provider,omitempty"`
    MaxTokens        int       `json:"max_tokens,omitempty"`

}

type Message struct {
    Role    string `json:"role"`    // "user", "assistant"
    Content string `json:"content"`
}
```

**Response:**
```go
type Response struct {
    RequestID      string `json:"request_id"`
    Content        string `json:"content"`
    Provider       string `json:"provider"`      // Which provider answered
    Attempts       int    `json:"attempts"`      // How many tries
    TotalLatencyMs int64  `json:"total_latency_ms"`
}
```

**Attempt** (tracks each retry):
```go
type Attempt struct {
    RequestID     string `json:"request_id"`
    AttemptNumber int    `json:"attempt_number"`
    Provider      string `json:"provider"`
    Status        string `json:"status"`         // "success" or "failed"
    ErrorType     string `json:"error_type,omitempty"`
    LatencyMs     int64  `json:"latency_ms"`
}
```

---

### 2. Validator (`internal/validator/validator.go`)

**Purpose:** Fail fast on invalid requests (Type 1 failures from ValidationFailures.md)

```go
func (v *Validator) Validate(req *Request) error {
    // Check required fields
    if req.Model == "" {
        return errors.New("model is required")
    }

    if len(req.Messages) == 0 {
        return errors.New("at least one message required")
    }

    // Check temperature range
    if req.Temperature < 0 || req.Temperature > 2 {
        return errors.New("temperature must be 0-2")
    }

    // Simple token estimation (char count / 4)
    estimatedTokens := estimateTokens(req.Messages)
    if estimatedTokens > modelLimits[req.Model] {
        return errors.New("token limit exceeded")
    }

    return nil
}
```

**Demo Impact:** Invalid requests fail in ~5ms, no wasted API calls.

---

### 3. Circuit Breaker (`internal/client/circuit_breaker.go`)

**Purpose:** Track provider health, skip failing providers

```go
type CircuitBreaker struct {
    state        string  // "closed" or "open"
    failureCount int
    threshold    int     // Open after N failures (e.g., 5)
}

func (cb *CircuitBreaker) IsOpen() bool {
    return cb.state == "open"
}

func (cb *CircuitBreaker) RecordFailure() {
    cb.failureCount++
    if cb.failureCount >= cb.threshold {
        cb.state = "open"
        log.Warn("Circuit breaker opened")
    }
}

func (cb *CircuitBreaker) RecordSuccess() {
    cb.failureCount = 0
    cb.state = "closed"
}
```

**Demo Impact:** After 5 failures to OpenAI, circuit opens. Next requests skip OpenAI entirely, use Anthropic.

---

### 4. HTTP Client (`internal/client/client.go`)

**Purpose:** Make HTTP calls with timeout handling

```go
type Client struct {
    httpClient      *http.Client
    circuitBreakers map[string]*CircuitBreaker
}

func (c *Client) Send(provider string, req Request) (*Response, error) {
    // Check circuit breaker
    if c.circuitBreakers[provider].IsOpen() {
        return nil, errors.New("circuit breaker open")
    }

    // Make HTTP call with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    resp, err := c.httpClient.Do(httpReq.WithContext(ctx))

    if err != nil {
        c.circuitBreakers[provider].RecordFailure()
        return nil, err
    }

    c.circuitBreakers[provider].RecordSuccess()
    return parseResponse(resp)
}
```

**Handles:** Timeouts, 429 rate limits, 5xx errors

---

### 5. Provider Router (`internal/router/router.go`)

**Purpose:** Select which provider to use (failover logic)

```go
type Router struct {
    providers map[string][]string  // model -> [provider1, provider2]
}

func (r *Router) SelectProvider(model string, attemptNum int, previousProvider string) string {
    eligible := r.providers[model]  // ["openai", "anthropic"]

    // First attempt: use first provider
    if attemptNum == 1 {
        return eligible[0]  // "openai"
    }

    // Retry: use different provider (failover)
    for _, p := range eligible {
        if p != previousProvider {
            return p  // "anthropic"
        }
    }

    return eligible[0]  // fallback
}
```

**Demo Impact:** Rate limit on attempt 1 (OpenAI) → attempt 2 uses Anthropic.

---

### 6. Orchestrator (`internal/orchestrator/orchestrator.go`)

**Purpose:** The brain. Coordinates retries, backoff, failover.

```go
type Orchestrator struct {
    validator *Validator
    client    *Client
    router    *Router
    maxRetries int
}

func (o *Orchestrator) Execute(req *Request) (*Response, error) {
    // Generate unique request ID
    requestID := generateID()

    // Validate
    if err := o.validator.Validate(req); err != nil {
        return nil, err  // Fast fail
    }

    var lastErr error
    var lastProvider string

    // Retry loop
    for attempt := 1; attempt <= o.maxRetries; attempt++ {
        // Select provider (failover on retry)
        provider := o.router.SelectProvider(req.Model, attempt, lastProvider)
        lastProvider = provider

        log.Info("Attempt", attempt, "using provider", provider)

        // Try request
        resp, err := o.client.Send(provider, req)
        if err == nil {
            // Success!
            resp.RequestID = requestID
            resp.Attempts = attempt
            return resp, nil
        }

        lastErr = err

        // Check if retryable
        if !isRetryable(err) {
            break  // Don't retry validation errors, 401, etc.
        }

        // Calculate backoff: 2s, 4s, 8s
        backoff := calculateBackoff(attempt)
        log.Warn("Attempt", attempt, "failed, waiting", backoff)
        time.Sleep(backoff)
    }

    return nil, fmt.Errorf("all retries failed: %v", lastErr)
}

func calculateBackoff(attempt int) time.Duration {
    // Exponential: 2^attempt seconds
    // Attempt 1: 2s, Attempt 2: 4s, Attempt 3: 8s
    base := 2.0
    seconds := math.Pow(base, float64(attempt))
    return time.Duration(seconds) * time.Second
}

func isRetryable(err error) bool {
    // Retry: timeouts, 429, 5xx
    // Don't retry: validation errors, 401, 400
    return strings.Contains(err.Error(), "timeout") ||
           strings.Contains(err.Error(), "429") ||
           strings.Contains(err.Error(), "500")
}
```

**This is the core logic that makes everything work together.**

---

### 7. API Handler (`internal/api/handlers.go`)

**Purpose:** HTTP endpoint that receives requests

```go
func ChatCompletionHandler(orch *Orchestrator) gin.HandlerFunc {
    return func(c *gin.Context) {
        var req models.Request

        // Parse JSON
        if err := c.BindJSON(&req); err != nil {
            c.JSON(400, gin.H{"error": "invalid request"})
            return
        }

        // Execute
        resp, err := orch.Execute(&req)
        if err != nil {
            c.JSON(500, gin.H{"error": err.Error()})
            return
        }

        c.JSON(200, resp)
    }
}
```

---

### 8. Mock Provider (`cmd/mock-provider/main.go`)

**Purpose:** Simulate LLM providers with configurable failures

```go
var config = FailureConfig{
    TimeoutProbability:    0.0,
    RateLimitProbability:  0.0,
    ServerErrorProbability: 0.0,
}

func chatHandler(c *gin.Context) {
    // Simulate timeout
    if rand.Float64() < config.TimeoutProbability {
        time.Sleep(40 * time.Second)  // Longer than client timeout
        return
    }

    // Simulate rate limit
    if rand.Float64() < config.RateLimitProbability {
        c.JSON(429, gin.H{"error": "rate limit exceeded"})
        return
    }

    // Simulate server error
    if rand.Float64() < config.ServerErrorProbability {
        c.JSON(500, gin.H{"error": "internal server error"})
        return
    }

    // Normal response
    c.JSON(200, gin.H{
        "id": "mock-123",
        "choices": []gin.H{{
            "message": gin.H{
                "role": "assistant",
                "content": "Mock LLM response",
            },
        }},
        "usage": gin.H{"total_tokens": 50},
    })
}

// Endpoint to configure failures during demo
func configHandler(c *gin.Context) {
    c.BindJSON(&config)
    c.JSON(200, gin.H{"status": "configured"})
}
```

---

## Build Order

Follow this sequence for fastest path to demo:

### Phase 1: Mock Provider (2-3h)
Build `cmd/mock-provider/main.go` first so you can test against it.

### Phase 2: Basic Flow (4-5h)
1. Config (hardcoded is fine)
2. Models (mostly done)
3. Validator (basic checks)
4. HTTP Client (without circuit breaker yet)
5. API Handler
6. Wire up in `cmd/server/main.go`

**Test:** Scenario 1 (happy path) works

### Phase 3: Retry Logic (3-4h)
1. Add retry loop to Orchestrator
2. Exponential backoff calculation
3. Better logging

**Test:** Scenario 2 (timeout + retry) works

### Phase 4: Failover (3-4h)
1. Provider Router
2. Update Orchestrator to switch providers

**Test:** Scenario 3 (rate limit + failover) works

### Phase 5: Circuit Breaker (3-4h)
1. Circuit Breaker component
2. Integrate into Client

**Test:** Scenario 4 (circuit breaker) works

### Phase 6: Polish (2-3h)
1. JSON logging with request IDs
2. Test all scenarios
3. Write README

**Total: ~18-20 hours**

---

## Running the Demo

```bash
# Terminal 1: Start mock provider
cd cmd/mock-provider && go run main.go

# Terminal 2: Start orchestrator
cd cmd/server && go run main.go

# Terminal 3: Run scenarios
# Scenario 1: Normal
curl -X POST http://localhost:8080/v1/chat/completions \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}'

# Scenario 2: Configure timeout, send request
curl -X POST http://localhost:8001/config/failures \
  -d '{"timeout_probability":0.7}'
curl -X POST http://localhost:8080/v1/chat/completions \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}'

# Watch logs show: timeout → retry → backoff → eventually success
```

See `DemoScenarios.md` for full demo script.

---

## Key Design Decisions

**Why exponential backoff?**
Prevents overwhelming a struggling provider. 2s, 4s, 8s gives it time to recover.

**Why circuit breaker per provider?**
Isolates failures. If OpenAI is down, we can still use Anthropic.

**Why fail-fast validation?**
Saves money (no wasted API calls) and gives instant feedback.

**Why request ID tracking?**
Makes debugging easy - same ID across all 3 retry attempts.

**Why mock provider first?**
Can test everything without real API keys or costs.

---

## What We're NOT Building (to save time)

- ❌ Half-open circuit breaker state (just closed/open)
- ❌ Advanced token counting (simple char/4 is fine)
- ❌ Persistent state (in-memory is fine for demo)
- ❌ Streaming support
- ❌ Authentication
- ❌ Request caching
- ❌ Metrics/monitoring

**Focus:** Make the 5 scenarios work perfectly with clean logs.

---

## Success Criteria

By the end, you can:

✅ Start 3 terminals (mock, server, curl)
✅ Run all 5 scenarios smoothly
✅ Logs clearly show retry, failover, circuit breaker
✅ Explain what's happening as you demo
✅ Handle questions about design decisions

**This shows you understand distributed systems resilience patterns in a working implementation.**
