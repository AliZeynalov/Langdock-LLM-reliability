import express, { Request, Response } from 'express';

const app = express();
app.use(express.json());

const PORT = process.env.PORT || 8001;

// Utility functions
function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

function generateId(): string {
  return Math.random().toString(36).substring(2, 10);
}

// Error response handler
function handleFailure(res: Response, failType: string): void | Promise<void> {
  switch (failType) {
    case '429':
      res.status(429).json({
        error: {
          message: 'Rate limit exceeded',
          type: 'rate_limit_error',
          code: 'rate_limit_exceeded',
        },
      });
      return;

    case '500':
      res.status(500).json({
        error: {
          message: 'Internal server error',
          type: 'server_error',
          code: 'internal_error',
        },
      });
      return;

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
      if (!isNaN(statusCode) && statusCode >= 100 && statusCode < 600) {
        res.status(statusCode).json({
          error: {
            message: `Simulated ${statusCode} error`,
            type: 'simulated_error',
          },
        });
        return;
      }
      res.status(400).json({
        error: {
          message: `Unknown fail type: ${failType}`,
        },
      });
  }
}

// Normal (non-streaming) response
function handleNormalResponse(res: Response, model: string): void {
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

  res.json(response);
}

// Streaming response (SSE)
async function handleStreaming(res: Response, failChunk?: string): Promise<void> {
  // Set SSE headers
  res.setHeader('Content-Type', 'text/event-stream');
  res.setHeader('Cache-Control', 'no-cache');
  res.setHeader('Connection', 'keep-alive');

  const chunks = ['Hello', ' from', ' streaming', ' mock', ' provider', '!'];
  const failAt = failChunk ? parseInt(failChunk, 10) : -1;

  for (let i = 0; i < chunks.length; i++) {
    // Simulate failure at specific chunk
    if (failAt > 0 && i + 1 === failAt) {
      // Send malformed JSON to simulate mid-stream failure
      res.write('data: {"broken": \n\n');
      res.end();
      return;
    }

    const data = JSON.stringify({
      id: `mock-${i}`,
      object: 'chat.completion.chunk',
      created: Math.floor(Date.now() / 1000),
      model: 'gpt-4',
      choices: [
        {
          index: 0,
          delta: { content: chunks[i] },
          finish_reason: null,
        },
      ],
    });

    res.write(`data: ${data}\n\n`);

    // Simulate generation delay between chunks
    await sleep(100);
  }

  // Send final chunk with finish_reason
  const finalChunk = JSON.stringify({
    id: `mock-final`,
    object: 'chat.completion.chunk',
    created: Math.floor(Date.now() / 1000),
    model: 'gpt-4',
    choices: [
      {
        index: 0,
        delta: {},
        finish_reason: 'stop',
      },
    ],
  });
  res.write(`data: ${finalChunk}\n\n`);
  res.write('data: [DONE]\n\n');
  res.end();
}

// Main request handler
async function handleChatCompletion(req: Request, res: Response): Promise<void> {
  const delay = req.query.delay as string | undefined;
  const fail = req.query.fail as string | undefined;
  const stream = req.query.stream as string | undefined;
  const failChunk = req.query.fail_chunk as string | undefined;

  console.log(`[${new Date().toISOString()}] POST /v1/chat/completions`, {
    delay,
    fail,
    stream,
    failChunk,
  });

  // Apply delay if specified
  if (delay) {
    const ms = parseInt(delay, 10);
    if (!isNaN(ms) && ms > 0) {
      await sleep(ms);
    }
  }

  // Simulate failures
  if (fail) {
    await handleFailure(res, fail);
    return;
  }

  // Streaming or normal response
  if (stream === 'true') {
    await handleStreaming(res, failChunk);
  } else {
    handleNormalResponse(res, req.body?.model);
  }
}

// Routes
app.post('/v1/chat/completions', handleChatCompletion);

app.get('/health', (_req, res) => {
  res.json({ status: 'healthy', timestamp: new Date().toISOString() });
});

// Start server
app.listen(PORT, () => {
  console.log(`🚀 Mock LLM Provider running on http://localhost:${PORT}`);
  console.log(`\nAvailable endpoints:`);
  console.log(`  POST /v1/chat/completions  - Chat completion endpoint`);
  console.log(`  GET  /health               - Health check\n`);
  console.log(`Query parameters for failure simulation:`);
  console.log(`  ?delay=N         - Add N milliseconds delay`);
  console.log(`  ?fail=429        - Return rate limit error`);
  console.log(`  ?fail=500        - Return server error`);
  console.log(`  ?fail=timeout    - Simulate 30s timeout`);
  console.log(`  ?stream=true     - Return streaming SSE response`);
  console.log(`  ?fail_chunk=N    - Fail on Nth chunk (streaming only)\n`);
});

