# Demo Scenarios - Quick Reference Guide

**Total Time:** 10 minutes  
**Goal:** Show retry, failover, and circuit breaker working

---

## Setup (30 seconds before demo)

```bash
# Terminal 1: Mock Provider
cd cmd/mock-provider && go run main.go

# Terminal 2: Orchestrator (MAIN - make this big/visible)
cd cmd/server && go run main.go

# Terminal 3: Commands
curl http://localhost:8080/health  # Should return {"status":"healthy"}
```

---

## Scenario 1: Normal Success (30 sec)

**Goal:** Show baseline - request succeeds on first try

```bash
# Reset mock to normal
curl -X POST http://localhost:8001/config/failures -d '{}'

# Send request
curl -X POST http://localhost:8080/v1/chat/completions -d '{
  "model":"gpt-4",
  "messages":[{"role":"user","content":"test"}]
}'
```

**What to point out in logs:**
- ✅ Request ID assigned
- ✅ Attempt 1 succeeds
- ✅ Fast latency (~1-2s)

**What to say:**
> "Normal flow: validation passes, one attempt, success. Notice the request_id tracks everything."

---

## Scenario 2: Timeout → Retry (1-2 min)

**Goal:** Show exponential backoff in action

```bash
# Configure timeouts
curl -X POST http://localhost:8001/config/failures -d '{
  "timeout_probability":0.7,
  "timeout_duration_seconds":3
}'

# Send request (will timeout and retry)
curl -X POST http://localhost:8080/v1/chat/completions -d '{
  "model":"gpt-4",
  "messages":[{"role":"user","content":"test"}]
}'
```

**What to point out in logs:**
- ✅ Attempt 1: timeout
- ✅ Wait 2.3 seconds
- ✅ Attempt 2: timeout
- ✅ Wait 4.7 seconds (doubled!)
- ✅ Attempt 3: success

**What to say:**
> "Provider times out. System retries with exponential backoff: 2s, then 4s. Prevents overwhelming a struggling provider. Eventually succeeds."

---

## Scenario 3: Rate Limit → Failover (1 min)

**Goal:** Show automatic provider switching

```bash
# Configure rate limits
curl -X POST http://localhost:8001/config/failures -d '{
  "rate_limit_probability":1.0
}'

# Send request (will failover)
curl -X POST http://localhost:8080/v1/chat/completions -d '{
  "model":"gpt-4",
  "messages":[{"role":"user","content":"test"}]
}'
```

**What to point out in logs:**
- ✅ Attempt 1: rate_limit (429) from openai
- ✅ Failover to anthropic
- ✅ Attempt 2: success from anthropic

**What to say:**
> "Hit rate limit on OpenAI. Instead of waiting, immediately fails over to Anthropic. Fast recovery, transparent to user."

---

## Scenario 4: Circuit Breaker (2 min)

**Goal:** Show protection from cascading failures

```bash
# Configure all errors
curl -X POST http://localhost:8001/config/failures -d '{
  "server_error_probability":1.0
}'

# Send 7 requests rapidly
for i in {1..7}; do
  curl -X POST http://localhost:8080/v1/chat/completions \
    -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}' &
done
wait
```

**What to point out in logs:**
- ✅ Requests 1-5: all fail (server errors)
- ✅ Circuit breaker OPENS (log message)
- ✅ Requests 6-7: skip openai immediately, use anthropic

**What to say:**
> "After 5 failures, circuit breaker opens. Subsequent requests don't even try OpenAI - they immediately use Anthropic. This prevents wasting time on a dead provider."

---

## Scenario 5: Validation (30 sec - bonus)

**Goal:** Show fail-fast on invalid input

```bash
# Invalid request (empty messages)
curl -X POST http://localhost:8080/v1/chat/completions -d '{
  "model":"gpt-4",
  "messages":[]
}'
```

**What to point out:**
- ✅ Instant error (<10ms)
- ✅ Clear error message
- ✅ No provider call made

**What to say:**
> "Invalid requests fail instantly. Saves API costs and gives immediate feedback."

---

## Cheat Sheet (Print This!)

```bash
# BEFORE DEMO:
Terminal 1: cd cmd/mock-provider && go run main.go
Terminal 2: cd cmd/server && go run main.go  
Terminal 3: curl http://localhost:8080/health

# SCENARIO 1: Normal
curl -X POST http://localhost:8001/config/failures -d '{}'
curl -X POST http://localhost:8080/v1/chat/completions -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}'

# SCENARIO 2: Timeout
curl -X POST http://localhost:8001/config/failures -d '{"timeout_probability":0.7,"timeout_duration_seconds":3}'
curl -X POST http://localhost:8080/v1/chat/completions -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}'

# SCENARIO 3: Rate Limit
curl -X POST http://localhost:8001/config/failures -d '{"rate_limit_probability":1.0}'
curl -X POST http://localhost:8080/v1/chat/completions -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}'

# SCENARIO 4: Circuit Breaker
curl -X POST http://localhost:8001/config/failures -d '{"server_error_probability":1.0}'
for i in {1..7}; do curl -X POST http://localhost:8080/v1/chat/completions -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}' & done; wait
```

---

## Key Talking Points

**For each scenario, mention:**
1. **What's happening** (timeout, rate limit, etc.)
2. **How system responds** (retry, failover, circuit breaker)
3. **The outcome** (eventual success, protection, transparency)

**General points to make:**
- Same request_id tracks entire journey
- Exponential backoff prevents overwhelming providers
- Failover is transparent to users
- Circuit breaker learns and protects
- System eventually succeeds or fails gracefully

---

## If Something Breaks

**Demo fails?** → Show code walkthrough instead  
**Logs unclear?** → Explain architecture from diagram  
**Time runs out?** → Skip validation scenario

**Remember:** Even showing the code + architecture is impressive! 

---

## Success = You Can:

✅ Start all 3 terminals without looking  
✅ Run all 4 main scenarios smoothly  
✅ Point out key logs without hesitation  
✅ Explain what's happening as you type  
✅ Handle questions confidently