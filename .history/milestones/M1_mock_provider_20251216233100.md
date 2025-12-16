# M1: Mock Provider

**Goal:** Build a fake LLM provider that simulates real provider behavior with configurable failures.

**Time:** 3-4 hours  
**Output:** HTTP server on :8001 that responds to chat completion requests

---

## Why Mock Provider First?

- Foundation for testing everything else
- No API keys needed
- Controllable failures for demo scenarios
- Can test gateway without external dependencies

---

## API Endpoint

```
POST /v1/chat/completions
Content-Type: application/json

{
  "model": "gpt-4",
  "messages": [{"role": "user", "content": "Hello"}],
  "stream": false
}
```

---

## Query Parameters for Failure Simulation

| Parameter | Example | Behavior |
|-----------|---------|----------|
| `delay` | `?delay=3000` | Adds latency in milliseconds before responding |
| `fail` | `?fail=429` | Returns specified HTTP error code |
| `fail` | `?fail=500` | Returns 500 Internal Server Error |
| `fail` | `?fail=timeout` | Waits 30 seconds (simulates timeout) |
| `stream` | `?stream=true` | Returns SSE streaming response |
| `fail_chunk` | `?fail_chunk=3` | In streaming mode, fails on chunk N |

---

## Response Format

### Non-Streaming Response

```json
{
  "id": "mock-12345",
  "object": "chat.completion",
  "created": 1699000000,
  "model": "gpt-4",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! I'm a mock LLM response. How can I help you today?"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 15,
    "total_tokens": 25
  }
}
```

### Streaming Response (SSE)

```
data: {"id":"mock-1","choices":[{"delta":{"content":"Hello"}}]}

data: {"id":"mock-2","choices":[{"delta":{"content":" world"}}]}

data: {"id":"mock-3","choices":[{"delta":{"content":"!"}}]}

data: [DONE]
```

### Error Responses

```json
// 429 Rate Limit
{
  "error": {
    "message": "Rate limit exceeded",
    "type": "rate_limit_error",
    "code": "rate_limit_exceeded"
  }
}

// 500 Server Error
{
  "error": {
    "message": "Internal server error",
    "type": "server_error",
    "code": "internal_error"
  }
}
```

---

## File Structure

```
src/
  mock-provider.ts    # Mock LLM server entry point
```

For M1, we keep it simple - one file. We can refactor later if needed.

---

## Implementation Steps

### Step 1: Project Setup

```bash
mkdir llm-reliability-gateway
cd llm-reliability-gateway
npm init -y
npm install express
npm install -D typescript @types/express @types/node ts-node
npx tsc --init
```

### Step 2: Basic Express Server

```typescript
// src/mock-provider.ts
import express, { Request, Response } from 'express';

const app = express();
app.use(express.json());

const PORT = process.env.PORT || 8001;

app.post('/v1/chat/completions', handleChatCompletion);

app.listen(PORT, () => {
  console.log(`Mock provider running on port ${PORT}`);
});
```

### Step 3: Parse Query Parameters

```typescript
async function handleChatCompletion(req: Request, res: Response) {
  // Get failure simulation params
  const delay = req.query.delay as string | undefined;
  const fail = req.query.fail as string | undefined;
  const stream = req.query.stream as string | undefined;
  const failChunk = req.query.fail_chunk as string | undefined;

  // Apply delay if specified
  if (delay) {
    const ms = parseInt(delay, 10);
    await sleep(ms);
  }

  // Simulate failures
  if (fail) {
    return handleFailure(res, fail);
  }

  // Normal response
  if (stream === 'true') {
    return handleStreaming(res, failChunk);
  } else {
    return handleNormalResponse(res, req.body.model);
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}
```

### Step 4: Implement Failure Modes

```typescript
function handleFailure(res: Response, failType: string) {
  switch (failType) {
    case '429':
      return res.status(429).json({
        error: {
          message: 'Rate limit exceeded',
          type: 'rate_limit_error',
          code: 'rate_limit_exceeded',
        },
      });

    case '500':
      return res.status(500).json({
        error: {
          message: 'Internal server error',
          type: 'server_error',
          code: 'internal_error',
        },
      });

    case 'timeout':
      // Sleep for 30 seconds to simulate timeout
      return sleep(30000).then(() => {
        res.status(504).json({
          error: {
            message: 'Gateway timeout',
            type: 'timeout_error',
          },
        });
      });

    default:
      // Try to parse as a status code
      const statusCode = parseInt(failType, 10);
      if (!isNaN(statusCode)) {
        return res.status(statusCode).json({
          error: {
            message: `Simulated ${statusCode} error`,
            type: 'simulated_error',
          },
        });
      }
      return res.status(400).json({
        error: {
          message: `Unknown fail type: ${failType}`,
        },
      });
  }
}
```

### Step 5: Implement Normal Response

```typescript
function generateId(): string {
  return Math.random().toString(36).substring(2, 10);
}

function handleNormalResponse(res: Response, model: string) {
  const response = {
    id: `mock-${generateId()}`,
    object: 'chat.completion',
    created: Math.floor(Date.now() / 1000),
    model: model || 'gpt-4',
    choices: [
      {
        index: 0,
        message: {
          role: 'assistant',
          content: "Hello! I'm a mock LLM response. How can I help you today?",
        },
        finish_reason: 'stop',
      },
    ],
    usage: {
      prompt_tokens: 10,
      completion_tokens: 15,
      total_tokens: 25,
    },
  };

  return res.json(response);
}
```

### Step 6: Implement Streaming Response

```typescript
async function handleStreaming(res: Response, failChunk?: string) {
  // Set SSE headers
  res.setHeader('Content-Type', 'text/event-stream');
  res.setHeader('Cache-Control', 'no-cache');
  res.setHeader('Connection', 'keep-alive');

  const chunks = ['Hello', ' from', ' streaming', ' mock', ' provider', '!'];
  const failAt = failChunk ? parseInt(failChunk, 10) : -1;

  for (let i = 0; i < chunks.length; i++) {
    // Simulate failure at specific chunk
    if (failAt > 0 && i + 1 === failAt) {
      // Send malformed JSON to simulate failure
      res.write('data: {"broken": \n\n');
      return res.end();
    }

    const data = JSON.stringify({
      id: `mock-${i}`,
      choices: [{ delta: { content: chunks[i] } }],
    });

    res.write(`data: ${data}\n\n`);

    // Simulate generation delay
    await sleep(100);
  }

  res.write('data: [DONE]\n\n');
  res.end();
}
```

---

## Complete Implementation

```typescript
// src/mock-provider.ts
import express, { Request, Response } from 'express';

const app = express();
app.use(express.json());

const PORT = process.env.PORT || 8001;

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

function generateId(): string {
  return Math.random().toString(36).substring(2, 10);
}

function handleFailure(res: Response, failType: string) {
  switch (failType) {
    case '429':
      return res.status(429).json({
        error: {
          message: 'Rate limit exceeded',
          type: 'rate_limit_error',
          code: 'rate_limit_exceeded',
        },
      });

    case '500':
      return res.status(500).json({
        error: {
          message: 'Internal server error',
          type: 'server_error',
          code: 'internal_error',
        },
      });

    case 'timeout':
      return sleep(30000).then(() => {
        res.status(504).json({
          error: {
            message: 'Gateway timeout',
            type: 'timeout_error',
          },
        });
      });

    default:
      const statusCode = parseInt(failType, 10);
      if (!isNaN(statusCode)) {
        return res.status(statusCode).json({
          error: {
            message: `Simulated ${statusCode} error`,
            type: 'simulated_error',
          },
        });
      }
      return res.status(400).json({
        error: { message: `Unknown fail type: ${failType}` },
      });
  }
}

function handleNormalResponse(res: Response, model: string) {
  return res.json({
    id: `mock-${generateId()}`,
    object: 'chat.completion',
    created: Math.floor(Date.now() / 1000),
    model: model || 'gpt-4',
    choices: [
      {
        index: 0,
        message: {
          role: 'assistant',
          content: "Hello! I'm a mock LLM response. How can I help you today?",
        },
        finish_reason: 'stop',
      },
    ],
    usage: {
      prompt_tokens: 10,
      completion_tokens: 15,
      total_tokens: 25,
    },
  });
}

async function handleStreaming(res: Response, failChunk?: string) {
  res.setHeader('Content-Type', 'text/event-stream');
  res.setHeader('Cache-Control', 'no-cache');
  res.setHeader('Connection', 'keep-alive');

  const chunks = ['Hello', ' from', ' streaming', ' mock', ' provider', '!'];
  const failAt = failChunk ? parseInt(failChunk, 10) : -1;

  for (let i = 0; i < chunks.length; i++) {
    if (failAt > 0 && i + 1 === failAt) {
      res.write('data: {"broken": \n\n');
      return res.end();
    }

    const data = JSON.stringify({
      id: `mock-${i}`,
      choices: [{ delta: { content: chunks[i] } }],
    });
    res.write(`data: ${data}\n\n`);
    await sleep(100);
  }

  res.write('data: [DONE]\n\n');
  res.end();
}

async function handleChatCompletion(req: Request, res: Response) {
  const delay = req.query.delay as string | undefined;
  const fail = req.query.fail as string | undefined;
  const stream = req.query.stream as string | undefined;
  const failChunk = req.query.fail_chunk as string | undefined;

  if (delay) {
    await sleep(parseInt(delay, 10));
  }

  if (fail) {
    return handleFailure(res, fail);
  }

  if (stream === 'true') {
    return handleStreaming(res, failChunk);
  }

  return handleNormalResponse(res, req.body?.model);
}

app.post('/v1/chat/completions', handleChatCompletion);

app.get('/health', (req, res) => {
  res.json({ status: 'healthy' });
});

app.listen(PORT, () => {
  console.log(`Mock provider running on port ${PORT}`);
});
```

---

## Testing Commands

### Happy Path

```bash
curl -X POST http://localhost:8001/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
```

### With Delay

```bash
curl -X POST "http://localhost:8001/v1/chat/completions?delay=2000" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
```

### Rate Limit Error

```bash
curl -X POST "http://localhost:8001/v1/chat/completions?fail=429" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
```

### Streaming

```bash
curl -X POST "http://localhost:8001/v1/chat/completions?stream=true" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
```

### Streaming with Failure

```bash
curl -X POST "http://localhost:8001/v1/chat/completions?stream=true&fail_chunk=3" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
```

---

## Running the Mock Provider

```bash
# Development
npx ts-node src/mock-provider.ts

# Or add to package.json scripts:
# "mock": "ts-node src/mock-provider.ts"
npm run mock
```

---

## Definition of Done

- [ ] Server starts on port 8001
- [ ] Returns valid JSON response for normal requests
- [ ] `?delay=N` adds N milliseconds delay
- [ ] `?fail=429` returns rate limit error
- [ ] `?fail=500` returns server error
- [ ] `?stream=true` returns SSE chunks
- [ ] `?fail_chunk=N` fails on Nth chunk during streaming
- [ ] All responses match OpenAI API format
