# M6: Streaming Support

**Goal:** Handle SSE streaming responses with failure detection and recovery.

**Time:** 3-4 hours  
**Output:** Streaming requests forward chunks as they arrive with proper error handling

---

## What We're Building

```
Client ←──── Gateway ←──── Provider
       SSE         SSE
       
Chunk 1 ─────────────────▶ Chunk 1
Chunk 2 ─────────────────▶ Chunk 2
Chunk 3 ─────────────────▶ Chunk 3
[DONE]  ─────────────────▶ [DONE]
```

---

## Failure Scenarios

| Scenario | Detection | Response |
|----------|-----------|----------|
| **Stall** | 10s timeout per chunk | Return partial + timeout error |
| **Disconnect** | Connection closed unexpectedly | Return partial + disconnect error |
| **Malformed** | JSON parse error | Terminate + partial response |

---

## Key Concepts

### 1. Server-Sent Events (SSE)

SSE is a standard for pushing data from server to client:

```
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive

data: {"content": "Hello"}

data: {"content": " world"}

data: [DONE]
```

### 2. Chunk Timeout

Each chunk should arrive within a reasonable time. If not, assume the stream stalled:

```typescript
const CHUNK_TIMEOUT = 10000; // 10 seconds per chunk
```

### 3. Partial Response Handling

If streaming fails mid-way, return whatever content we received so far.

---

## Implementation

### Updated Types

```typescript
// src/types.ts

export interface StreamChunk {
  id: string;
  object: string;
  created: number;
  model: string;
  choices: Array<{
    index: number;
    delta: {
      role?: string;
      content?: string;
    };
    finish_reason: string | null;
  }>;
}
```

### Streaming Provider Client

```typescript
// src/provider/streamingClient.ts
import { Logger } from 'pino';
import { ChatRequest } from '../types';

export interface StreamCallbacks {
  onChunk: (chunk: string) => void;
  onDone: () => void;
  onError: (error: Error, partialContent: string) => void;
}

export class StreamingProviderClient {
  private timeout: number;
  private chunkTimeout: number;
  private logger: Logger;

  constructor(config: { timeout: number; chunkTimeout?: number }, logger: Logger) {
    this.timeout = config.timeout;
    this.chunkTimeout = config.chunkTimeout ?? 10000;
    this.logger = logger;
  }

  async streamRequest(
    requestId: string,
    url: string,
    req: ChatRequest,
    callbacks: StreamCallbacks
  ): Promise<void> {
    const controller = new AbortController();
    let chunkTimeoutId: NodeJS.Timeout | null = null;
    let partialContent = '';

    const resetChunkTimeout = () => {
      if (chunkTimeoutId) clearTimeout(chunkTimeoutId);
      chunkTimeoutId = setTimeout(() => {
        this.logger.warn({
          requestId,
          event: 'chunk_timeout',
        }, 'Chunk timeout - stream stalled');
        controller.abort();
        callbacks.onError(
          new Error('Stream stalled - chunk timeout'),
          partialContent
        );
      }, this.chunkTimeout);
    };

    try {
      const response = await fetch(`${url}/v1/chat/completions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...req, stream: true }),
        signal: controller.signal,
      });

      if (!response.ok) {
        throw new Error(`Provider returned ${response.status}`);
      }

      if (!response.body) {
        throw new Error('No response body');
      }

      // Start chunk timeout
      resetChunkTimeout();

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();

        if (done) {
          if (chunkTimeoutId) clearTimeout(chunkTimeoutId);
          callbacks.onDone();
          break;
        }

        // Reset timeout on each chunk
        resetChunkTimeout();

        buffer += decoder.decode(value, { stream: true });

        // Process complete SSE messages
        const lines = buffer.split('\n');
        buffer = lines.pop() || ''; // Keep incomplete line in buffer

        for (const line of lines) {
          if (line.startsWith('data: ')) {
            const data = line.slice(6).trim();

            if (data === '[DONE]') {
              if (chunkTimeoutId) clearTimeout(chunkTimeoutId);
              callbacks.onDone();
              return;
            }

            try {
              const parsed = JSON.parse(data);
              const content = parsed.choices?.[0]?.delta?.content;

              if (content) {
                partialContent += content;
                callbacks.onChunk(data);
              }
            } catch (parseError) {
              this.logger.warn({
                requestId,
                data,
                event: 'malformed_chunk',
              }, 'Malformed chunk received');

              if (chunkTimeoutId) clearTimeout(chunkTimeoutId);
              callbacks.onError(
                new Error('Malformed chunk'),
                partialContent
              );
              return;
            }
          }
        }
      }
    } catch (error) {
      if (chunkTimeoutId) clearTimeout(chunkTimeoutId);

      if (error instanceof Error && error.name === 'AbortError') {
        // Already handled by chunk timeout
        return;
      }

      this.logger.error({
        requestId,
        error: error instanceof Error ? error.message : 'Unknown error',
        event: 'stream_error',
      }, 'Stream error');

      callbacks.onError(
        error instanceof Error ? error : new Error('Stream failed'),
        partialContent
      );
    }
  }
}
```

### Streaming Handler

```typescript
// src/handlers/streaming.ts
import { Request, Response } from 'express';
import { Logger } from 'pino';
import { StreamingProviderClient } from '../provider/streamingClient';
import { ChatRequest } from '../types';

export function createStreamingHandler(
  client: StreamingProviderClient,
  providerUrl: string,
  logger: Logger
) {
  return async (req: Request, res: Response) => {
    const requestId = req.requestId;
    const chatRequest: ChatRequest = req.body;

    logger.info({
      requestId,
      model: chatRequest.model,
      event: 'stream_started',
    }, 'Starting stream');

    // Set SSE headers
    res.setHeader('Content-Type', 'text/event-stream');
    res.setHeader('Cache-Control', 'no-cache');
    res.setHeader('Connection', 'keep-alive');
    res.setHeader('X-Request-ID', requestId);

    // Prevent buffering
    res.flushHeaders();

    await client.streamRequest(requestId, providerUrl, chatRequest, {
      onChunk: (chunk) => {
        res.write(`data: ${chunk}\n\n`);
      },

      onDone: () => {
        logger.info({
          requestId,
          event: 'stream_completed',
        }, 'Stream completed');
        res.write('data: [DONE]\n\n');
        res.end();
      },

      onError: (error, partialContent) => {
        logger.error({
          requestId,
          error: error.message,
          partialLength: partialContent.length,
          event: 'stream_error',
        }, 'Stream error');

        // Send error event to client
        const errorEvent = JSON.stringify({
          error: {
            message: error.message,
            type: 'stream_error',
            partial_content: partialContent,
          },
        });
        res.write(`data: ${errorEvent}\n\n`);
        res.end();
      },
    });
  };
}
```

### Updated Gateway Routes

```typescript
// src/index.ts (additions)
import { StreamingProviderClient } from './provider/streamingClient';
import { createStreamingHandler } from './handlers/streaming';

// ... existing setup ...

const streamingClient = new StreamingProviderClient(
  { timeout: 30000, chunkTimeout: 10000 },
  logger
);

// Route based on stream parameter
app.post('/v1/chat/completions', validationMiddleware(logger), async (req, res) => {
  if (req.body.stream === true) {
    // Handle streaming request
    const streamHandler = createStreamingHandler(
      streamingClient,
      process.env.OPENAI_URL || 'http://localhost:8001',
      logger
    );
    return streamHandler(req, res);
  }

  // Handle non-streaming request (existing code)
  // ...
});
```

---

## Mock Provider Streaming (from M1)

The mock provider already supports streaming:

```typescript
// Query params:
// ?stream=true           - Enable streaming
// ?fail_chunk=3          - Fail on chunk 3 (malformed JSON)

// Example streaming response:
data: {"id":"mock-0","choices":[{"delta":{"content":"Hello"}}]}

data: {"id":"mock-1","choices":[{"delta":{"content":" from"}}]}

data: {"id":"mock-2","choices":[{"delta":{"content":" streaming"}}]}

data: [DONE]
```

---

## Testing

### Test 1: Happy Path Streaming

```bash
curl -N -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}], "stream": true}'

# Expected: Chunks arrive progressively, ends with [DONE]
```

### Test 2: Stream with Failure

```bash
# Configure mock to fail on chunk 3
curl -N -X POST "http://localhost:8001/v1/chat/completions?stream=true&fail_chunk=3" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'

# Expected: First 2 chunks, then error event with partial content
```

### Test 3: Stream Timeout (Stall)

```bash
# Configure mock to delay chunks significantly
# Gateway should timeout after 10s per chunk
```

### Test 4: Client Disconnect

```bash
# Start streaming request
curl -N -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}], "stream": true}'

# Press Ctrl+C mid-stream
# Check logs show proper cleanup
```

---

## Expected Logs

### Successful Stream

```json
{"level":30,"requestId":"req_abc123","model":"gpt-4","event":"stream_started","msg":"Starting stream"}
{"level":30,"requestId":"req_abc123","event":"stream_completed","msg":"Stream completed"}
```

### Stream with Error

```json
{"level":30,"requestId":"req_def456","model":"gpt-4","event":"stream_started","msg":"Starting stream"}
{"level":40,"requestId":"req_def456","data":"{\"broken\":","event":"malformed_chunk","msg":"Malformed chunk received"}
{"level":50,"requestId":"req_def456","error":"Malformed chunk","partialLength":24,"event":"stream_error","msg":"Stream error"}
```

---

## File Structure

```
src/
  provider/
    streamingClient.ts   # Streaming provider client
  handlers/
    streaming.ts         # Streaming request handler
```

---

## Definition of Done

- [ ] Streaming requests forward SSE chunks to client
- [ ] Non-streaming requests work as before
- [ ] Chunk timeout (10s) detects stalled streams
- [ ] Malformed chunks terminate stream with error
- [ ] Partial content returned on failure
- [ ] Proper SSE headers set
- [ ] X-Request-ID header in streaming response
- [ ] Logs show stream_started, stream_completed, stream_error events
- [ ] Client disconnect handled gracefully

