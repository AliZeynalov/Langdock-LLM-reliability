# M5: Request Validation

**Goal:** Reject invalid requests before making expensive API calls.

**Time:** 2-3 hours  
**Output:** Validation fails fast with clear error messages

---

## What We're Building

```
Request arrives
      │
      ▼
┌─────────────┐
│  Validator  │ ─── Invalid ──▶ 400 Bad Request (< 10ms)
│    (zod)    │                  No provider call!
└──────┬──────┘
       │ Valid
       ▼
   Continue to provider
```

---

## What We Validate

| Field | Rule | Error |
|-------|------|-------|
| `model` | Required, non-empty | "model is required" |
| `messages` | Required, non-empty array | "messages is required" |
| `messages[].role` | Must be "user", "assistant", or "system" | "invalid role" |
| `messages[].content` | Required, non-empty | "message content is required" |
| `temperature` | 0.0 to 2.0 (if provided) | "temperature must be between 0 and 2" |
| `max_tokens` | > 0 (if provided) | "max_tokens must be positive" |

---

## Why Zod?

- TypeScript-first validation
- Excellent error messages
- Type inference from schemas
- Composable schemas
- No need for manual type guards

```typescript
// Schema defines validation AND TypeScript types
const schema = z.object({
  model: z.string(),
  messages: z.array(MessageSchema),
});

// Infer TypeScript type from schema
type Request = z.infer<typeof schema>;
```

---

## Implementation

### Install Zod

```bash
npm install zod
```

### Validation Schema

```typescript
// src/validator/schemas.ts
import { z } from 'zod';

// Message schema
export const MessageSchema = z.object({
  role: z.enum(['user', 'assistant', 'system'], {
    errorMap: () => ({ message: 'role must be user, assistant, or system' }),
  }),
  content: z.string().min(1, 'content is required'),
});

// Chat request schema
export const ChatRequestSchema = z.object({
  model: z.string().min(1, 'model is required'),
  messages: z
    .array(MessageSchema)
    .min(1, 'messages is required and cannot be empty'),
  temperature: z
    .number()
    .min(0, 'temperature must be at least 0')
    .max(2, 'temperature must be at most 2')
    .optional(),
  max_tokens: z
    .number()
    .int('max_tokens must be an integer')
    .positive('max_tokens must be positive')
    .optional(),
  stream: z.boolean().optional(),
});

// Infer TypeScript types from schemas
export type Message = z.infer<typeof MessageSchema>;
export type ChatRequest = z.infer<typeof ChatRequestSchema>;
```

### Validator Function

```typescript
// src/validator/validator.ts
import { z } from 'zod';
import { ChatRequestSchema } from './schemas';

export interface ValidationError {
  field: string;
  message: string;
}

export interface ValidationResult {
  success: boolean;
  errors?: ValidationError[];
  data?: z.infer<typeof ChatRequestSchema>;
}

export function validateRequest(body: unknown): ValidationResult {
  const result = ChatRequestSchema.safeParse(body);

  if (result.success) {
    return {
      success: true,
      data: result.data,
    };
  }

  // Transform Zod errors into our format
  const errors: ValidationError[] = result.error.errors.map((err) => ({
    field: err.path.join('.') || 'body',
    message: err.message,
  }));

  return {
    success: false,
    errors,
  };
}
```

### Validation Middleware

```typescript
// src/middleware/validation.ts
import { Request, Response, NextFunction } from 'express';
import { Logger } from 'pino';
import { validateRequest } from '../validator/validator';

export function validationMiddleware(logger: Logger) {
  return (req: Request, res: Response, next: NextFunction) => {
    const requestId = req.requestId;
    const result = validateRequest(req.body);

    if (!result.success) {
      logger.warn({
        requestId,
        errors: result.errors,
        event: 'validation_failed',
      }, 'Request validation failed');

      return res.status(400).json({
        error: {
          type: 'validation_error',
          message: 'Request validation failed',
          details: result.errors,
        },
      });
    }

    // Attach validated data to request
    req.body = result.data;
    next();
  };
}
```

### Using in Gateway

```typescript
// src/index.ts
import express from 'express';
import pino from 'pino';
import { requestIdMiddleware } from './middleware/requestId';
import { loggingMiddleware } from './middleware/logging';
import { validationMiddleware } from './middleware/validation';
import { ProviderClient } from './provider/client';

const logger = pino({ level: process.env.LOG_LEVEL || 'info' });

const app = express();
app.use(express.json());
app.use(requestIdMiddleware);
app.use(loggingMiddleware(logger));

const providerClient = new ProviderClient({
  openaiUrl: process.env.OPENAI_URL || 'http://localhost:8001',
  anthropicUrl: process.env.ANTHROPIC_URL || 'http://localhost:8002',
  timeout: 10000,
}, logger);

// Apply validation middleware to chat completions route
app.post(
  '/v1/chat/completions',
  validationMiddleware(logger),
  async (req, res) => {
    const startTime = Date.now();
    const requestId = req.requestId;

    try {
      logger.info({
        requestId,
        model: req.body.model,
        event: 'validated',
      }, 'Request validated');

      const response = await providerClient.call(requestId, req.body);

      res.json({
        requestId,
        content: response.content,
        model: req.body.model,
        provider: response.provider,
        attempts: response.attempts,
        totalLatencyMs: Date.now() - startTime,
        createdAt: new Date().toISOString(),
      });
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
  }
);

app.get('/health', (req, res) => {
  res.json({ status: 'healthy' });
});

app.listen(8080, () => {
  logger.info({ port: 8080 }, 'Gateway started');
});
```

---

## Error Response Format

```json
{
  "error": {
    "type": "validation_error",
    "message": "Request validation failed",
    "details": [
      {
        "field": "model",
        "message": "model is required"
      },
      {
        "field": "messages",
        "message": "messages is required and cannot be empty"
      }
    ]
  }
}
```

---

## Testing

### Test 1: Missing Model

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"messages": [{"role": "user", "content": "Hello"}]}'

# Expected: 400 with "model is required"
```

### Test 2: Empty Messages

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": []}'

# Expected: 400 with "messages is required and cannot be empty"
```

### Test 3: Invalid Role

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "invalid", "content": "Hello"}]}'

# Expected: 400 with "role must be user, assistant, or system"
```

### Test 4: Invalid Temperature

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}], "temperature": 5.0}'

# Expected: 400 with "temperature must be at most 2"
```

### Test 5: Multiple Errors

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"messages": [{"role": "invalid", "content": ""}]}'

# Expected: 400 with multiple errors:
# - model is required
# - role must be user, assistant, or system
# - content is required
```

### Test 6: Valid Request (Sanity Check)

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'

# Expected: 200, normal response
```

### Test 7: Valid Request with Optional Fields

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}],
    "temperature": 0.7,
    "max_tokens": 100
  }'

# Expected: 200, normal response
```

---

## Expected Logs

### Invalid Request

```json
{"level":30,"requestId":"req_abc123","method":"POST","path":"/v1/chat/completions","event":"started","msg":"Request started"}
{"level":40,"requestId":"req_abc123","errors":[{"field":"model","message":"model is required"}],"event":"validation_failed","msg":"Request validation failed"}
{"level":30,"requestId":"req_abc123","status":400,"latencyMs":2,"event":"completed","msg":"Request completed"}
```

### Valid Request

```json
{"level":30,"requestId":"req_def456","method":"POST","path":"/v1/chat/completions","event":"started","msg":"Request started"}
{"level":30,"requestId":"req_def456","model":"gpt-4","event":"validated","msg":"Request validated"}
{"level":30,"requestId":"req_def456","provider":"openai","attempt":1,"event":"attempt","msg":"Attempting request"}
{"level":30,"requestId":"req_def456","event":"success","msg":"Request succeeded"}
{"level":30,"requestId":"req_def456","status":200,"latencyMs":150,"event":"completed","msg":"Request completed"}
```

---

## File Structure

```
src/
  validator/
    schemas.ts     # Zod schemas
    validator.ts   # Validation function
  middleware/
    validation.ts  # Validation middleware
```

---

## Definition of Done

- [ ] Model required validation
- [ ] Messages required validation
- [ ] Message role validation (user/assistant/system)
- [ ] Message content required validation
- [ ] Temperature range validation (0-2)
- [ ] MaxTokens positive validation
- [ ] Returns 400 with detailed error messages
- [ ] No provider call made on validation failure
- [ ] Response time < 10ms for validation errors
- [ ] Logs show validation_failed event
