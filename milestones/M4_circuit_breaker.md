# M4: Circuit Breaker + Failover

**Goal:** Skip broken providers and failover to healthy ones.

**Time:** 3-4 hours  
**Output:** Circuit breaker prevents calling broken provider, router handles failover

---

## What We're Building

```
                    ┌─────────────────┐
                    │     Router      │
                    │ (picks provider)│
                    └────────┬────────┘
                             │
           ┌─────────────────┴─────────────────┐
           │                                   │
    ┌──────▼──────┐                     ┌──────▼──────┐
    │   Circuit   │                     │   Circuit   │
    │   Breaker   │                     │   Breaker   │
    │  (OpenAI)   │                     │ (Anthropic) │
    └──────┬──────┘                     └──────┬──────┘
           │                                   │
    ┌──────▼──────┐                     ┌──────▼──────┐
    │   OpenAI    │                     │  Anthropic  │
    │   :8001     │                     │   :8002     │
    └─────────────┘                     └─────────────┘
```

---

## Circuit Breaker States (Simplified)

We use 2 states only (no half-open for simplicity):

```
┌──────────┐         3 consecutive      ┌──────────┐
│  CLOSED  │ ─────── failures ────────▶ │   OPEN   │
│ (healthy)│                            │ (broken) │
└──────────┘ ◀──────── 30 seconds ───── └──────────┘
                     auto-reset
```

| State | Behavior |
|-------|----------|
| **CLOSED** | Normal - allow requests, track failures |
| **OPEN** | Broken - reject requests immediately, skip to next provider |

---

## Key Concepts

### 1. Circuit Breaker Class

Tracks consecutive failures for a provider. Opens after threshold reached.

```typescript
// src/provider/circuitBreaker.ts
import { Logger } from 'pino';

export type CircuitState = 'closed' | 'open';

export interface CircuitBreakerConfig {
  failureThreshold: number;  // Opens after this many failures (default: 3)
  resetTimeout: number;      // Auto-close after this duration in ms (default: 30000)
}

export class CircuitBreaker {
  private name: string;
  private state: CircuitState = 'closed';
  private consecutiveFails: number = 0;
  private lastFailure: number = 0;
  private config: CircuitBreakerConfig;
  private logger: Logger;

  constructor(name: string, logger: Logger, config?: Partial<CircuitBreakerConfig>) {
    this.name = name;
    this.logger = logger;
    this.config = {
      failureThreshold: config?.failureThreshold ?? 3,
      resetTimeout: config?.resetTimeout ?? 30000,
    };
  }

  /**
   * Check if request should proceed
   */
  canExecute(): boolean {
    if (this.state === 'closed') {
      return true;
    }

    // Check if reset timeout has passed
    const timeSinceFailure = Date.now() - this.lastFailure;
    if (timeSinceFailure > this.config.resetTimeout) {
      this.state = 'closed';
      this.consecutiveFails = 0;

      this.logger.info({
        provider: this.name,
        event: 'circuit_reset',
      }, 'Circuit breaker reset to closed');

      return true;
    }

    return false;
  }

  /**
   * Record a successful request - resets failure count
   */
  recordSuccess(): void {
    this.consecutiveFails = 0;
  }

  /**
   * Record a failed request - may open circuit
   */
  recordFailure(): void {
    this.consecutiveFails++;
    this.lastFailure = Date.now();

    if (this.consecutiveFails >= this.config.failureThreshold) {
      this.state = 'open';

      this.logger.warn({
        provider: this.name,
        failures: this.consecutiveFails,
        event: 'circuit_opened',
      }, 'Circuit breaker opened');
    }
  }

  /**
   * Check if circuit is open
   */
  isOpen(): boolean {
    return this.state === 'open';
  }

  /**
   * Get current state for metrics/debugging
   */
  getState(): CircuitState {
    return this.state;
  }

  /**
   * Get provider name
   */
  getName(): string {
    return this.name;
  }
}
```

### 2. Provider Definition

```typescript
// src/provider/router.ts
import { CircuitBreaker } from './circuitBreaker';

export interface Provider {
  name: string;
  url: string;
  circuit: CircuitBreaker;
}
```

### 3. Router (Provider Selection + Failover)

Selects provider and handles failover:

```typescript
// src/provider/router.ts
import { Logger } from 'pino';
import { CircuitBreaker } from './circuitBreaker';

export interface Provider {
  name: string;
  url: string;
  circuit: CircuitBreaker;
}

export class Router {
  private providers: Provider[];
  private logger: Logger;

  constructor(
    openaiUrl: string,
    anthropicUrl: string,
    logger: Logger
  ) {
    this.logger = logger;
    this.providers = [
      {
        name: 'openai',
        url: openaiUrl,
        circuit: new CircuitBreaker('openai', logger),
      },
      {
        name: 'anthropic',
        url: anthropicUrl,
        circuit: new CircuitBreaker('anthropic', logger),
      },
    ];
  }

  /**
   * Get first healthy provider
   */
  getHealthyProvider(): Provider | null {
    for (const provider of this.providers) {
      if (provider.circuit.canExecute()) {
        return provider;
      }
    }
    return null;
  }

  /**
   * Get next provider after the given one (for failover)
   */
  getNextProvider(currentName: string): Provider | null {
    let foundCurrent = false;

    // Look for providers after current
    for (const provider of this.providers) {
      if (provider.name === currentName) {
        foundCurrent = true;
        continue;
      }
      if (foundCurrent && provider.circuit.canExecute()) {
        return provider;
      }
    }

    // Wrap around - check providers before current
    for (const provider of this.providers) {
      if (provider.name === currentName) {
        break;
      }
      if (provider.circuit.canExecute()) {
        return provider;
      }
    }

    return null;
  }

  /**
   * Get all providers (for metrics/debugging)
   */
  getAllProviders(): Provider[] {
    return this.providers;
  }
}
```

### 4. Updated Client with Router

```typescript
// src/provider/client.ts
import { Logger } from 'pino';
import { ChatRequest, ProviderResponse } from '../types';
import { ProviderError } from './errors';
import { Router, Provider } from './router';

export interface ProviderConfig {
  openaiUrl: string;
  anthropicUrl: string;
  timeout: number;
}

export interface RetryConfig {
  maxAttempts: number;
  baseDelay: number;
  maxDelay: number;
}

export class ProviderClient {
  private router: Router;
  private timeout: number;
  private retryConfig: RetryConfig;
  private logger: Logger;

  constructor(
    config: ProviderConfig,
    logger: Logger,
    retryConfig?: Partial<RetryConfig>
  ) {
    this.logger = logger;
    this.timeout = config.timeout;
    this.router = new Router(config.openaiUrl, config.anthropicUrl, logger);
    this.retryConfig = {
      maxAttempts: retryConfig?.maxAttempts ?? 3,
      baseDelay: retryConfig?.baseDelay ?? 1000,
      maxDelay: retryConfig?.maxDelay ?? 10000,
    };
  }

  async call(requestId: string, req: ChatRequest): Promise<ProviderResponse> {
    // Get initial provider
    let provider = this.router.getHealthyProvider();
    if (!provider) {
      throw new Error('No healthy providers available');
    }

    let totalAttempts = 0;
    let lastError: Error | null = null;

    while (provider) {
      this.logger.info({
        requestId,
        provider: provider.name,
        event: 'selected',
      }, 'Provider selected');

      try {
        // Try this provider with retries
        const result = await this.callWithRetry(requestId, req, provider);
        return {
          ...result,
          attempts: totalAttempts + result.attempts,
        };
      } catch (error) {
        lastError = error as Error;
        totalAttempts += this.retryConfig.maxAttempts;

        // Check if we should failover
        if (error instanceof ProviderError && error.failover) {
          this.logger.info({
            requestId,
            fromProvider: provider.name,
            event: 'failover',
          }, 'Triggering failover');

          // Get next provider
          const nextProvider = this.router.getNextProvider(provider.name);
          if (!nextProvider) {
            this.logger.warn({
              requestId,
              event: 'no_failover_provider',
            }, 'No failover provider available');
            break;
          }

          this.logger.info({
            requestId,
            toProvider: nextProvider.name,
            event: 'failover',
          }, 'Failing over to next provider');

          provider = nextProvider;
          continue;
        }

        // Non-failover error - stop trying
        break;
      }
    }

    throw new Error(`All attempts failed: ${lastError?.message}`);
  }

  private async callWithRetry(
    requestId: string,
    req: ChatRequest,
    provider: Provider
  ): Promise<ProviderResponse> {
    let lastError: Error | null = null;

    for (let attempt = 1; attempt <= this.retryConfig.maxAttempts; attempt++) {
      this.logger.info({
        requestId,
        provider: provider.name,
        attempt,
        event: 'attempt',
      }, 'Attempting request');

      try {
        const response = await this.doRequest(req, provider.url);

        // Success - record and return
        provider.circuit.recordSuccess();

        return {
          ...response,
          provider: provider.name,
          attempts: attempt,
        };
      } catch (error) {
        lastError = error as Error;

        // Record failure
        provider.circuit.recordFailure();

        if (error instanceof ProviderError) {
          // 429 - don't retry, trigger failover
          if (error.failover) {
            throw error;
          }

          // Not retryable - return error
          if (!error.retryable) {
            throw error;
          }
        }

        this.logger.warn({
          requestId,
          provider: provider.name,
          attempt,
          error: lastError.message,
          event: 'attempt_failed',
        }, 'Attempt failed');

        // Wait before retry (unless last attempt)
        if (attempt < this.retryConfig.maxAttempts) {
          const wait = this.calculateBackoff(attempt);

          this.logger.info({
            requestId,
            waitMs: wait,
            event: 'backoff',
          }, 'Waiting before retry');

          await this.sleep(wait);
        }
      }
    }

    throw new Error(`Exhausted ${this.retryConfig.maxAttempts} attempts: ${lastError?.message}`);
  }

  private async doRequest(
    req: ChatRequest,
    url: string
  ): Promise<Omit<ProviderResponse, 'provider' | 'attempts'>> {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), this.timeout);

    try {
      const response = await fetch(`${url}/v1/chat/completions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(req),
        signal: controller.signal,
      });

      clearTimeout(timeoutId);

      if (response.status >= 500) {
        const body = await response.text();
        throw ProviderError.serverError(response.status, body);
      }

      if (response.status === 429) {
        const body = await response.text();
        throw ProviderError.rateLimitError(body);
      }

      if (response.status >= 400) {
        const body = await response.text();
        throw ProviderError.clientError(response.status, body);
      }

      const data = await response.json() as {
        choices?: Array<{ message?: { content?: string } }>;
      };

      return {
        content: data.choices?.[0]?.message?.content || '',
      };
    } catch (error) {
      clearTimeout(timeoutId);

      if (error instanceof Error && error.name === 'AbortError') {
        throw ProviderError.timeoutError();
      }

      if (error instanceof TypeError) {
        throw ProviderError.networkError(error.message);
      }

      if (error instanceof ProviderError) {
        throw error;
      }

      throw error;
    }
  }

  private calculateBackoff(attempt: number): number {
    let wait = this.retryConfig.baseDelay * Math.pow(2, attempt - 1);
    wait = Math.min(wait, this.retryConfig.maxDelay);
    const jitter = 1.0 + (Math.random() * 0.4 - 0.2);
    return Math.floor(wait * jitter);
  }

  private sleep(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }

  /**
   * Get router for metrics/debugging
   */
  getRouter(): Router {
    return this.router;
  }
}
```

---

## Testing

### Setup: Two Mock Providers

```bash
# Terminal 1: Mock OpenAI on :8001
PORT=8001 npx ts-node src/mock-provider.ts

# Terminal 2: Mock Anthropic on :8002
PORT=8002 npx ts-node src/mock-provider.ts

# Terminal 3: Gateway
npx ts-node src/index.ts
```

### Test 1: Circuit Breaker Opens

```bash
# Send requests that fail with 500
# Note: You'll need to configure mock to fail
for i in {1..4}; do
  curl -X POST "http://localhost:8080/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
  echo ""
done
```

**Expected:** After 3 failures on OpenAI, circuit opens, 4th request goes to Anthropic.

### Test 2: Rate Limit Failover

```bash
# Configure OpenAI mock to return 429
curl -X POST "http://localhost:8001/v1/chat/completions?fail=429" ...

# Through gateway - should failover to anthropic
curl -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'

# Logs should show:
# selected provider=openai
# received 429
# failover to anthropic
# selected provider=anthropic
# success
```

### Test 3: All Providers Down

```bash
# Configure both mocks to return 500
# After circuit breaks on both:
curl -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'

# Should return:
# {"error": {"message": "No healthy providers available"}}
```

### Test 4: Circuit Reset

```bash
# Break OpenAI circuit with 500s
# Wait 30 seconds
# Next request should try OpenAI again (circuit reset)
```

---

## Expected Logs (Circuit Breaker + Failover)

```json
{"level":30,"requestId":"req_a1b2","provider":"openai","event":"selected","msg":"Provider selected"}
{"level":30,"requestId":"req_a1b2","provider":"openai","attempt":1,"event":"attempt","msg":"Attempting request"}
{"level":40,"requestId":"req_a1b2","provider":"openai","attempt":1,"error":"Server error 500","event":"attempt_failed","msg":"Attempt failed"}
{"level":30,"requestId":"req_a1b2","waitMs":1000,"event":"backoff","msg":"Waiting before retry"}
{"level":30,"requestId":"req_a1b2","provider":"openai","attempt":2,"event":"attempt","msg":"Attempting request"}
{"level":40,"requestId":"req_a1b2","provider":"openai","attempt":2,"error":"Server error 500","event":"attempt_failed","msg":"Attempt failed"}
{"level":30,"requestId":"req_a1b2","waitMs":2000,"event":"backoff","msg":"Waiting before retry"}
{"level":30,"requestId":"req_a1b2","provider":"openai","attempt":3,"event":"attempt","msg":"Attempting request"}
{"level":40,"requestId":"req_a1b2","provider":"openai","attempt":3,"error":"Server error 500","event":"attempt_failed","msg":"Attempt failed"}
{"level":40,"provider":"openai","failures":3,"event":"circuit_opened","msg":"Circuit breaker opened"}
{"level":30,"requestId":"req_a1b2","fromProvider":"openai","event":"failover","msg":"Triggering failover"}
{"level":30,"requestId":"req_a1b2","toProvider":"anthropic","event":"failover","msg":"Failing over to next provider"}
{"level":30,"requestId":"req_a1b2","provider":"anthropic","event":"selected","msg":"Provider selected"}
{"level":30,"requestId":"req_a1b2","provider":"anthropic","attempt":1,"event":"attempt","msg":"Attempting request"}
{"level":30,"requestId":"req_a1b2","event":"success","msg":"Request succeeded"}
```

---

## Definition of Done

- [ ] CircuitBreaker tracks consecutive failures per provider
- [ ] Circuit opens after 3 failures
- [ ] Circuit auto-resets after 30 seconds
- [ ] Router returns healthy provider
- [ ] Router handles failover to next provider
- [ ] 429 triggers immediate failover (no retry)
- [ ] 5xx triggers retry then failover
- [ ] Logs show circuit state changes
- [ ] Logs show failover events
- [ ] Returns error when all providers unavailable
