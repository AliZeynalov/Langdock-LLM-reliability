# M2: Gateway + Simple Client

**Goal:** Build the gateway server that receives requests and forwards them to a provider.

**Time:** 3-4 hours  
**Output:** Gateway on :8080 that forwards to mock provider on :8001

---

## What We're Building

```
┌──────────┐      ┌──────────────┐      ┌───────────────┐
│  Client  │ ──── │   Gateway    │ ──── │ Mock Provider │
│  (curl)  │      │   (:8080)    │      │   (:8001)     │
└──────────┘      └──────────────┘      └───────────────┘
                         │
                  • Request ID middleware
                  • Structured logging (pino)
                  • Provider client (fetch)
```

---

## Key Concepts

### 1. Request ID Middleware

Every request gets a unique ID that travels through the entire lifecycle.

```typescript
// src/middleware/requestId.ts
import { Request, Response, NextFunction } from 'express';
import { randomUUID } from 'crypto';

declare global {
  namespace Express {
    interface Request {
      requestId: string;
    }
  }
}

export function requestIdMiddleware(req: Request, res: Response, next: NextFunction) {
  // Generate unique ID: "req_a1b2c3d4"
  const requestId = `req_${randomUUID().substring(0, 8)}`;

  // Attach to request object
  req.requestId = requestId;

  // Also return in response header (for client debugging)
  res.setHeader('X-Request-ID', requestId);

  next();
}
```

**Why this matters:** When you see logs, you can filter by requestId to see the entire journey of one request.

### 2. Structured Logging with Pino

Instead of `console.log`, use structured logs:

```typescript
import pino from 'pino';

const logger = pino({
  level: process.env.LOG_LEVEL || 'info',
});

// Bad - hard to parse
console.log('Request received for model gpt-4');

// Good - structured, searchable
logger.info({
  requestId,
  model: request.model,
  event: 'received',
}, 'Request received');

// Output (JSON):
// {"level":30,"time":1699000000000,"requestId":"req_a1b2c3d4","model":"gpt-4","event":"received","msg":"Request received"}
```

### 3. Fetch-based Provider Client

Node.js 18+ has built-in fetch API:

```typescript
const response = await fetch(url, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(requestBody),
  signal: AbortSignal.timeout(10000), // 10s timeout
});
```

---

## File Structure

```
src/
  index.ts              # Gateway entry point
  types.ts              # Shared types
  middleware/
    requestId.ts        # Request ID middleware
    logging.ts          # Logging middleware
  provider/
    client.ts           # HTTP client to call providers
```

---

## Implementation Steps

### Step 1: Install Dependencies

```bash
npm install pino pino-pretty
npm install -D @types/node
```

### Step 2: Define Types

```typescript
// src/types.ts

export interface Message {
  role: 'user' | 'assistant' | 'system';
  content: string;
}

export interface ChatRequest {
  model: string;
  messages: Message[];
  temperature?: number;
  max_tokens?: number;
  stream?: boolean;
}

export interface ChatResponse {
  requestId: string;
  content: string;
  model: string;
  provider: string;
  attempts: number;
  totalLatencyMs: number;
  createdAt: string;
}

export interface ProviderResponse {
  content: string;
  provider: string;
  attempts: number;
}
```

### Step 3: Gateway Entry Point

```typescript
// src/index.ts
import express from 'express';
import pino from 'pino';
import { requestIdMiddleware } from './middleware/requestId';
import { loggingMiddleware } from './middleware/logging';
import { ProviderClient } from './provider/client';
import { ChatRequest, ChatResponse } from './types';

const logger = pino({
  level: process.env.LOG_LEVEL || 'info',
});

const app = express();
app.use(express.json());
app.use(requestIdMiddleware);
app.use(loggingMiddleware(logger));

const PORT = process.env.PORT || 8080;

// Create provider client
const providerClient = new ProviderClient({
  openaiUrl: process.env.OPENAI_URL || 'http://localhost:8001',
  anthropicUrl: process.env.ANTHROPIC_URL || 'http://localhost:8002',
  timeout: 10000, // 10 seconds
}, logger);

// Routes
app.post('/v1/chat/completions', async (req, res) => {
  const startTime = Date.now();
  const requestId = req.requestId;

  try {
    const chatRequest: ChatRequest = req.body;

    logger.info({
      requestId,
      model: chatRequest.model,
      event: 'validated',
    }, 'Request validated');

    // Call provider
    const providerResponse = await providerClient.call(requestId, chatRequest);

    // Build response
    const response: ChatResponse = {
      requestId,
      content: providerResponse.content,
      model: chatRequest.model,
      provider: providerResponse.provider,
      attempts: providerResponse.attempts,
      totalLatencyMs: Date.now() - startTime,
      createdAt: new Date().toISOString(),
    };

    res.json(response);
  } catch (error) {
    logger.error({
      requestId,
      error: error instanceof Error ? error.message : 'Unknown error',
      event: 'error',
    }, 'Request failed');

    res.status(502).json({
      error: {
        message: error instanceof Error ? error.message : 'Provider error',
        type: 'gateway_error',
      },
    });
  }
});

app.get('/health', (req, res) => {
  res.json({ status: 'healthy' });
});

app.listen(PORT, () => {
  logger.info({ port: PORT }, 'Gateway started');
});
```

### Step 4: Request ID Middleware

```typescript
// src/middleware/requestId.ts
import { Request, Response, NextFunction } from 'express';
import { randomUUID } from 'crypto';

declare global {
  namespace Express {
    interface Request {
      requestId: string;
    }
  }
}

export function requestIdMiddleware(
  req: Request,
  res: Response,
  next: NextFunction
) {
  const requestId = `req_${randomUUID().substring(0, 8)}`;
  req.requestId = requestId;
  res.setHeader('X-Request-ID', requestId);
  next();
}
```

### Step 5: Logging Middleware

```typescript
// src/middleware/logging.ts
import { Request, Response, NextFunction } from 'express';
import { Logger } from 'pino';

export function loggingMiddleware(logger: Logger) {
  return (req: Request, res: Response, next: NextFunction) => {
    const startTime = Date.now();
    const requestId = req.requestId;

    logger.info({
      requestId,
      method: req.method,
      path: req.path,
      event: 'started',
    }, 'Request started');

    // Log on response finish
    res.on('finish', () => {
      logger.info({
        requestId,
        status: res.statusCode,
        latencyMs: Date.now() - startTime,
        event: 'completed',
      }, 'Request completed');
    });

    next();
  };
}
```

### Step 6: Provider Client

```typescript
// src/provider/client.ts
import { Logger } from 'pino';
import { ChatRequest, ProviderResponse } from '../types';

export interface ProviderConfig {
  openaiUrl: string;
  anthropicUrl: string;
  timeout: number;
}

export class ProviderClient {
  private config: ProviderConfig;
  private logger: Logger;

  constructor(config: ProviderConfig, logger: Logger) {
    this.config = config;
    this.logger = logger;
  }

  async call(requestId: string, req: ChatRequest): Promise<ProviderResponse> {
    this.logger.info({
      requestId,
      provider: 'openai',
      event: 'calling',
    }, 'Calling provider');

    const startTime = Date.now();

    const response = await fetch(`${this.config.openaiUrl}/v1/chat/completions`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(req),
      signal: AbortSignal.timeout(this.config.timeout),
    });

    const latency = Date.now() - startTime;

    if (!response.ok) {
      const errorBody = await response.text();
      this.logger.warn({
        requestId,
        statusCode: response.status,
        body: errorBody,
        event: 'provider_error',
      }, 'Provider returned error');

      throw new Error(`Provider returned ${response.status}: ${errorBody}`);
    }

    // Parse response (OpenAI format)
    const data = await response.json() as {
      choices?: Array<{
        message?: {
          content?: string;
        };
      }>;
    };

    const content = data.choices?.[0]?.message?.content || '';

    this.logger.info({
      requestId,
      provider: 'openai',
      latencyMs: latency,
      event: 'success',
    }, 'Provider call succeeded');

    return {
      content,
      provider: 'openai',
      attempts: 1,
    };
  }
}
```

---

## Testing

### 1. Start Mock Provider (from M1)

```bash
npx ts-node src/mock-provider.ts
# Running on :8001
```

### 2. Start Gateway

```bash
npx ts-node src/index.ts
# Running on :8080
```

### 3. Test Request Through Gateway

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
```

### Expected Response

```json
{
  "requestId": "req_a1b2c3d4",
  "content": "Hello! I'm a mock LLM response.",
  "model": "gpt-4",
  "provider": "openai",
  "attempts": 1,
  "totalLatencyMs": 150,
  "createdAt": "2024-12-17T10:00:00.000Z"
}
```

### Expected Logs (Pretty Printed)

```
INFO: Gateway started {"port":8080}
INFO: Request started {"requestId":"req_a1b2c3d4","method":"POST","path":"/v1/chat/completions","event":"started"}
INFO: Request validated {"requestId":"req_a1b2c3d4","model":"gpt-4","event":"validated"}
INFO: Calling provider {"requestId":"req_a1b2c3d4","provider":"openai","event":"calling"}
INFO: Provider call succeeded {"requestId":"req_a1b2c3d4","provider":"openai","latencyMs":150,"event":"success"}
INFO: Request completed {"requestId":"req_a1b2c3d4","status":200,"latencyMs":155,"event":"completed"}
```

---

## Running with Pretty Logs (Development)

```bash
# Install pino-pretty for readable logs
npm install -D pino-pretty

# Run with pretty output
npx ts-node src/index.ts | npx pino-pretty
```

---

## Package.json Scripts

```json
{
  "scripts": {
    "dev": "ts-node src/index.ts | pino-pretty",
    "mock": "ts-node src/mock-provider.ts",
    "start": "node dist/index.js"
  }
}
```

---

## Definition of Done

- [ ] Gateway starts on port 8080
- [ ] Request ID generated for each request
- [ ] Request ID visible in response header (X-Request-ID)
- [ ] Request ID visible in response body
- [ ] All logs include requestId field
- [ ] All logs are structured JSON (pino)
- [ ] Request forwards to mock provider
- [ ] Response returned to client
- [ ] Health endpoint works: GET /health
