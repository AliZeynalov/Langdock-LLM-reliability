# M3: Retry with Backoff

**Goal:** Add automatic retry logic with exponential backoff for transient failures.

**Time:** 3-4 hours  
**Output:** Failed requests automatically retry before giving up

---

## What We're Building

```
Request → Provider fails (timeout/5xx)
              ↓
         Wait 1s → Retry
              ↓
         Provider fails again
              ↓
         Wait 2s → Retry
              ↓
         Provider fails again
              ↓
         Wait 4s → Retry
              ↓
         Success (or give up after max attempts)
```

---

## Retry Strategy

| Error Type | Action | Reason |
|------------|--------|--------|
| Timeout | **Retry same provider** | Temporary network/server issue |
| 5xx (500, 502, 503, 504) | **Retry same provider** | Server temporarily overloaded |
| 429 Rate Limit | **Immediate failover** | Provider overloaded, try another |
| 4xx (400, 401, 403, 404) | **Fail fast** | Request is broken, won't help to retry |
| Success (2xx) | **Return response** | Done! |

---

## Key Concepts

### 1. Exponential Backoff

Wait time doubles after each failure:

```
Attempt 1: immediate
Attempt 2: wait 1 second
Attempt 3: wait 2 seconds
Attempt 4: wait 4 seconds (if max_attempts > 3)
```

Formula: `wait = baseDelay * 2^(attempt - 1)`

### 2. Jitter (Recommended)

Add randomness to prevent "thundering herd" when many requests retry at the same time:

```typescript
// Without jitter: all retries happen at exactly 1s, 2s, 4s
// With jitter: retries spread out (0.8s-1.2s, 1.6s-2.4s, etc.)

const jitter = Math.random() * 0.4 - 0.2; // -20% to +20%
const waitWithJitter = wait * (1 + jitter);
```

### 3. AbortController for Cancellation

Respect cancellation - if the client disconnects, stop retrying:

```typescript
const controller = new AbortController();

// Cancel after timeout
const timeoutId = setTimeout(() => controller.abort(), timeout);

try {
  const response = await fetch(url, { signal: controller.signal });
} finally {
  clearTimeout(timeoutId);
}
```

---

## Implementation

### Custom Error Types

```typescript
// src/provider/errors.ts

export type ErrorType = 
  | 'network'
  | 'timeout'
  | 'server_error'
  | 'rate_limit'
  | 'client_error'
  | 'decode';

export class ProviderError extends Error {
  readonly type: ErrorType;
  readonly statusCode?: number;
  readonly retryable: boolean;
  readonly failover: boolean;

  constructor(options: {
    type: ErrorType;
    message: string;
    statusCode?: number;
    retryable: boolean;
    failover?: boolean;
  }) {
    super(options.message);
    this.name = 'ProviderError';
    this.type = options.type;
    this.statusCode = options.statusCode;
    this.retryable = options.retryable;
    this.failover = options.failover ?? false;
  }

  static networkError(message: string): ProviderError {
    return new ProviderError({
      type: 'network',
      message,
      retryable: true,
    });
  }

  static timeoutError(): ProviderError {
    return new ProviderError({
      type: 'timeout',
      message: 'Request timed out',
      retryable: true,
    });
  }

  static serverError(statusCode: number, body: string): ProviderError {
    return new ProviderError({
      type: 'server_error',
      message: `Server error ${statusCode}: ${body}`,
      statusCode,
      retryable: true,
    });
  }

  static rateLimitError(body: string): ProviderError {
    return new ProviderError({
      type: 'rate_limit',
      message: `Rate limit exceeded: ${body}`,
      statusCode: 429,
      retryable: false,
      failover: true, // Trigger failover to another provider
    });
  }

  static clientError(statusCode: number, body: string): ProviderError {
    return new ProviderError({
      type: 'client_error',
      message: `Client error ${statusCode}: ${body}`,
      statusCode,
      retryable: false,
    });
  }
}
```

### Retry Configuration

```typescript
// src/provider/client.ts

export interface RetryConfig {
  maxAttempts: number;   // Max retry attempts (default: 3)
  baseDelay: number;     // Initial delay in ms (default: 1000)
  maxDelay: number;      // Cap on delay in ms (default: 10000)
}

const defaultRetryConfig: RetryConfig = {
  maxAttempts: 3,
  baseDelay: 1000,
  maxDelay: 10000,
};
```

### Updated Provider Client

```typescript
// src/provider/client.ts
import { Logger } from 'pino';
import { ChatRequest, ProviderResponse } from '../types';
import { ProviderError } from './errors';

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
  private config: ProviderConfig;
  private retryConfig: RetryConfig;
  private logger: Logger;

  constructor(
    config: ProviderConfig,
    logger: Logger,
    retryConfig?: Partial<RetryConfig>
  ) {
    this.config = config;
    this.logger = logger;
    this.retryConfig = {
      maxAttempts: retryConfig?.maxAttempts ?? 3,
      baseDelay: retryConfig?.baseDelay ?? 1000,
      maxDelay: retryConfig?.maxDelay ?? 10000,
    };
  }

  async call(requestId: string, req: ChatRequest): Promise<ProviderResponse> {
    let lastError: Error | null = null;

    for (let attempt = 1; attempt <= this.retryConfig.maxAttempts; attempt++) {
      this.logger.info({
        requestId,
        provider: 'openai',
        attempt,
        event: 'attempt',
      }, 'Attempting provider call');

      try {
        const response = await this.doRequest(requestId, req);

        this.logger.info({
          requestId,
          attempt,
          event: 'success',
        }, 'Request succeeded');

        return {
          ...response,
          attempts: attempt,
        };
      } catch (error) {
        lastError = error as Error;

        // Check if we should retry
        if (error instanceof ProviderError) {
          if (!error.retryable) {
            this.logger.warn({
              requestId,
              error: error.message,
              errorType: error.type,
              event: 'no_retry',
            }, 'Error not retryable');
            throw error;
          }
        }

        this.logger.warn({
          requestId,
          attempt,
          error: lastError.message,
          event: 'failed',
        }, 'Attempt failed');

        // Don't wait after the last attempt
        if (attempt === this.retryConfig.maxAttempts) {
          break;
        }

        // Calculate backoff and wait
        const waitDuration = this.calculateBackoff(attempt);

        this.logger.info({
          requestId,
          attempt,
          waitMs: waitDuration,
          event: 'backoff',
        }, 'Waiting before retry');

        await this.sleep(waitDuration);
      }
    }

    this.logger.warn({
      requestId,
      maxAttempts: this.retryConfig.maxAttempts,
      event: 'exhausted',
    }, 'All retry attempts exhausted');

    throw new Error(
      `All ${this.retryConfig.maxAttempts} attempts failed: ${lastError?.message}`
    );
  }

  private async doRequest(
    requestId: string,
    req: ChatRequest
  ): Promise<Omit<ProviderResponse, 'attempts'>> {
    const controller = new AbortController();
    const timeoutId = setTimeout(
      () => controller.abort(),
      this.config.timeout
    );

    try {
      const response = await fetch(
        `${this.config.openaiUrl}/v1/chat/completions`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(req),
          signal: controller.signal,
        }
      );

      clearTimeout(timeoutId);

      // Handle errors based on status code
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

      // Parse successful response
      const data = await response.json() as {
        choices?: Array<{
          message?: { content?: string };
        }>;
      };

      return {
        content: data.choices?.[0]?.message?.content || '',
        provider: 'openai',
      };
    } catch (error) {
      clearTimeout(timeoutId);

      // Handle abort (timeout)
      if (error instanceof Error && error.name === 'AbortError') {
        throw ProviderError.timeoutError();
      }

      // Handle network errors
      if (error instanceof TypeError) {
        throw ProviderError.networkError(error.message);
      }

      // Re-throw ProviderErrors as-is
      if (error instanceof ProviderError) {
        throw error;
      }

      throw error;
    }
  }

  private calculateBackoff(attempt: number): number {
    // Exponential: 1s, 2s, 4s, 8s...
    let wait = this.retryConfig.baseDelay * Math.pow(2, attempt - 1);

    // Cap at max delay
    wait = Math.min(wait, this.retryConfig.maxDelay);

    // Add jitter (-20% to +20%)
    const jitter = 1.0 + (Math.random() * 0.4 - 0.2);
    wait = Math.floor(wait * jitter);

    return wait;
  }

  private sleep(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }
}
```

---

## Testing

### Test 1: Retry on Timeout

```bash
# Start mock with 5s delay (gateway timeout is 3s)
# Mock will be called, timeout, retry, timeout, retry, timeout, fail

curl -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
```

To test this, configure the mock provider to delay:

```bash
# Start mock with delay
curl -X POST "http://localhost:8001/v1/chat/completions?delay=5000" ...
```

### Test 2: Retry on 500

```bash
# Configure mock to return 500
curl -X POST "http://localhost:8001/v1/chat/completions?fail=500" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'

# Watch gateway logs show:
# attempt=1 → 500 → backoff 1s
# attempt=2 → 500 → backoff 2s
# attempt=3 → 500 → exhausted
```

### Test 3: No Retry on 400

```bash
# This should fail immediately, no retries
curl -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d 'invalid json'

# Logs should show: no_retry, no backoff
```

### Test 4: Rate Limit Triggers Failover Flag

```bash
# Configure mock to return 429
curl -X POST "http://localhost:8001/v1/chat/completions?fail=429" ...

# Error should have failover=true (handled in M4)
```

---

## Expected Logs (Retry Scenario)

```json
{"level":30,"requestId":"req_abc123","provider":"openai","attempt":1,"event":"attempt","msg":"Attempting provider call"}
{"level":40,"requestId":"req_abc123","attempt":1,"error":"Server error 500","event":"failed","msg":"Attempt failed"}
{"level":30,"requestId":"req_abc123","attempt":1,"waitMs":1000,"event":"backoff","msg":"Waiting before retry"}
{"level":30,"requestId":"req_abc123","provider":"openai","attempt":2,"event":"attempt","msg":"Attempting provider call"}
{"level":40,"requestId":"req_abc123","attempt":2,"error":"Server error 500","event":"failed","msg":"Attempt failed"}
{"level":30,"requestId":"req_abc123","attempt":2,"waitMs":2000,"event":"backoff","msg":"Waiting before retry"}
{"level":30,"requestId":"req_abc123","provider":"openai","attempt":3,"event":"attempt","msg":"Attempting provider call"}
{"level":30,"requestId":"req_abc123","attempt":3,"event":"success","msg":"Request succeeded"}
```

---

## Pretty Log Output

With `pino-pretty`:

```
INFO: Attempting provider call {"requestId":"req_abc123","provider":"openai","attempt":1,"event":"attempt"}
WARN: Attempt failed {"requestId":"req_abc123","attempt":1,"error":"Server error 500","event":"failed"}
INFO: Waiting before retry {"requestId":"req_abc123","attempt":1,"waitMs":1000,"event":"backoff"}
INFO: Attempting provider call {"requestId":"req_abc123","provider":"openai","attempt":2,"event":"attempt"}
WARN: Attempt failed {"requestId":"req_abc123","attempt":2,"error":"Server error 500","event":"failed"}
INFO: Waiting before retry {"requestId":"req_abc123","attempt":2,"waitMs":2000,"event":"backoff"}
INFO: Attempting provider call {"requestId":"req_abc123","provider":"openai","attempt":3,"event":"attempt"}
INFO: Request succeeded {"requestId":"req_abc123","attempt":3,"event":"success"}
```

---

## Definition of Done

- [ ] Retry on network timeout
- [ ] Retry on 5xx errors (500, 502, 503, 504)
- [ ] No retry on 4xx errors (except 429 handled separately)
- [ ] Exponential backoff: 1s → 2s → 4s
- [ ] Jitter added to backoff times
- [ ] Max 3 attempts by default
- [ ] Logs show each attempt with requestId
- [ ] Logs show backoff duration
- [ ] Response includes `attempts` count
- [ ] 429 errors marked with `failover: true`
