package models

import "time"

// Message represents a single chat message
type Message struct {
    Role    string `json:"role"`    // "user", "assistant", "system"
    Content string `json:"content"`
}

// Request represents an incoming chat completion request
type Request struct {
    Model            string    `json:"model"`              // e.g., "gpt-4"
    Messages         []Message `json:"messages"`
    Temperature      float64   `json:"temperature,omitempty"`      // 0.0 - 2.0
    MaxTokens        int       `json:"max_tokens,omitempty"`
    Stream           bool      `json:"stream,omitempty"`
    PreferredProvider string   `json:"preferred_provider,omitempty"` // "openai", "anthropic"
}

// Response represents the final response to the client
type Response struct {
    RequestID       string    `json:"request_id"`
    Content         string    `json:"content"`
    Model           string    `json:"model"`
    Provider        string    `json:"provider"`
    Attempts        int       `json:"attempts"`
    TotalLatencyMs  int64     `json:"total_latency_ms"`
    TokensUsed      int       `json:"tokens_used,omitempty"`
    CreatedAt       time.Time `json:"created_at"`
}

// Attempt represents a single attempt to fulfill a request
type Attempt struct {
    RequestID      string    `json:"request_id"`
    AttemptNumber  int       `json:"attempt_number"`
    Provider       string    `json:"provider"`
    StartedAt      time.Time `json:"started_at"`
    EndedAt        time.Time `json:"ended_at"`
    Status         string    `json:"status"` // "success", "failed"
    ErrorType      string    `json:"error_type,omitempty"`
    ErrorMessage   string    `json:"error_message,omitempty"`
    LatencyMs      int64     `json:"latency_ms"`
    TokensUsed     int       `json:"tokens_used,omitempty"`
}
