# Langdock LLM Reliability System - Implementation Guide

**Language:** Go
**Goal:** Demo-ready MVP showing retry, failover, and circuit breaker
**Time Budget:** ~18-20 hours total
**Approach:** Build only what's needed for the 5 demo scenarios

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [Project Structure](#project-structure)
3. [Technology Stack](#technology-stack)
4. [MVP Scope](#mvp-scope)
5. [Implementation Plan](#implementation-plan)
6. [Core Components](#core-components)
7. [Testing Strategy](#testing-strategy)
8. [Demo Scenarios](#demo-scenarios)

---

## Quick Start

### Prerequisites
```bash
# Install Go 1.21+
go version

# Initialize project
mkdir langdock-llm-reliability
cd langdock-llm-reliability
go mod init github.com/yourusername/langdock-llm-reliability
```

### Core Dependencies
```bash
# HTTP router
go get github.com/gin-gonic/gin

# HTTP client (with retries)
go get github.com/hashicorp/go-retryablehttp

# Configuration
go get github.com/spf13/viper

# Logging
go get github.com/sirupsen/logrus
go get github.com/google/uuid

# Metrics
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promhttp

# Testing
go get github.com/stretchr/testify
```

---

## Project Structure

```
langdock-llm-reliability/
├── cmd/
│   ├── server/
│   │   └── main.go                 # Application entry point
│   └── mock-provider/
│       └── main.go                 # Mock LLM provider for testing
│
├── internal/
│   ├── models/
│   │   ├── request.go              # Request data structures
│   │   ├── response.go             # Response data structures
│   │   └── attempt.go              # Attempt tracking
│   │
│   ├── validator/
│   │   └── validator.go            # Request validation
│   │
│   ├── orchestrator/
│   │   ├── orchestrator.go         # Core orchestration logic
│   │   └── retry.go                # Retry policy
│   │
│   ├── router/
│   │   ├── router.go               # Provider routing
│   │   └── health.go               # Provider health tracking
│   │
│   ├── client/
│   │   ├── client.go               # HTTP client wrapper
│   │   ├── circuit_breaker.go      # Circuit breaker
│   │   └── adapters/
│   │       ├── adapter.go          # Base adapter interface
│   │       ├── openai.go           # OpenAI adapter
│   │       └── anthropic.go        # Anthropic adapter
│   │
│   ├── streaming/
│   │   └── handler.go              # Streaming handler
│   │
│   ├── observability/
│   │   ├── logger.go               # Structured logging
│   │   └── metrics.go              # Prometheus metrics
│   │
│   └── api/
│       └── handlers.go             # HTTP handlers
│
├── pkg/
│   └── errors/
│       └── errors.go               # Custom error types
│
├── config/
│   └── config.yaml                 # Configuration file
│
├── scripts/
│   ├── setup.sh                    # Setup script
│   └── test-scenarios.sh           # Test failure scenarios
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## Technology Stack

**Core:**
- **Go 1.21+** - Fast, concurrent, built for systems programming
- **Gin** - High-performance HTTP framework
- **net/http** - Native HTTP client (excellent for what we need)

**Observability:**
- **logrus** - Structured logging
- **prometheus/client_golang** - Metrics
- **UUID** - Request ID generation

**Configuration:**
- **viper** - Configuration management
- **YAML** - Config file format

**Testing:**
- **testify** - Assertion library
- **httptest** - HTTP testing utilities

**Why Go?**
- Native concurrency with goroutines (perfect for retries/streaming)
- Excellent HTTP libraries
- Fast compilation for rapid iteration
- Strong typing catches errors early
- Single binary deployment
- Great for building resilient distributed systems

---

## Core Components (Go Implementation)

### Component 1: Data Models

```go
// internal/models/request.go
package models

import (
	"time"
)

// Request represents an incoming LLM request
type Request struct {
	RequestID         string                 `json:"request_id"`
	ClientID          string                 `json:"client_id"`
	Model             string                 `json:"model"`
	Messages          []Message              `json:"messages"`
	Temperature       float64                `json:"temperature,omitempty"`
	MaxTokens         int                    `json:"max_tokens,omitempty"`
	Stream            bool                   `json:"stream,omitempty"`
	PreferredProvider string                 `json:"preferred_provider,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Attempt represents a single attempt to fulfill a request
type Attempt struct {
	RequestID     string        `json:"request_id"`
	AttemptNumber int           `json:"attempt_number"`
	Provider      string        `json:"provider"`
	StartedAt     time.Time     `json:"started_at"`
	EndedAt       *time.Time    `json:"ended_at,omitempty"`
	Status        string        `json:"status"` // in_progress, success, failed
	ErrorType     string        `json:"error_type,omitempty"`
	ErrorMessage  string        `json:"error_message,omitempty"`
	LatencyMs     int64         `json:"latency_ms,omitempty"`
	TokensUsed    int           `json:"tokens_used,omitempty"`
	HTTPStatus    int           `json:"http_status,omitempty"`
}

// Response represents the final response
type Response struct {
	RequestID  string    `json:"request_id"`
	Content    string    `json:"content"`
	Model      string    `json:"model"`
	Provider   string    `json:"provider"`
	Attempts   int       `json:"attempts"`
	TotalLatency int64   `json:"total_latency_ms"`
	TokensUsed int       `json:"tokens_used"`
	CreatedAt  time.Time `json:"created_at"`
}
```

### Component 2: Configuration

```go
// internal/config/config.go
package config

import (
	"time"
	"github.com/spf13/viper"
)

type Config struct {
	Server      ServerConfig      `mapstructure:"server"`
	Retry       RetryConfig       `mapstructure:"retry"`
	Timeout     TimeoutConfig     `mapstructure:"timeout"`
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	Streaming   StreamingConfig   `mapstructure:"streaming"`
	Providers   []ProviderConfig  `mapstructure:"providers"`
	Observability ObservabilityConfig `mapstructure:"observability"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type RetryConfig struct {
	MaxRetries       int           `mapstructure:"max_retries"`
	BaseBackoff      time.Duration `mapstructure:"base_backoff"`
	MaxBackoff       time.Duration `mapstructure:"max_backoff"`
	EnableFailover   bool          `mapstructure:"enable_failover"`
}

type TimeoutConfig struct {
	Connect time.Duration `mapstructure:"connect"`
	Read    time.Duration `mapstructure:"read"`
	Total   time.Duration `mapstructure:"total"`
}

type CircuitBreakerConfig struct {
	Threshold int           `mapstructure:"threshold"`
	Timeout   time.Duration `mapstructure:"timeout"`
}

type StreamingConfig struct {
	HeartbeatTimeout time.Duration `mapstructure:"heartbeat_timeout"`
	ValidateChunks   bool          `mapstructure:"validate_chunks"`
}

type ProviderConfig struct {
	Name     string   `mapstructure:"name"`
	BaseURL  string   `mapstructure:"base_url"`
	APIKey   string   `mapstructure:"api_key"`
	Models   []string `mapstructure:"models"`
	Priority int      `mapstructure:"priority"`
}

type ObservabilityConfig struct {
	LogLevel     string `mapstructure:"log_level"`
	MetricsPort  int    `mapstructure:"metrics_port"`
}

func Load(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.AutomaticEnv()
	
	// Set defaults
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("retry.max_retries", 3)
	viper.SetDefault("retry.base_backoff", "2s")
	viper.SetDefault("retry.max_backoff", "60s")
	viper.SetDefault("timeout.connect", "10s")
	viper.SetDefault("timeout.read", "30s")
	viper.SetDefault("timeout.total", "60s")
	
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	
	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}
	
	return &config, nil
}
```

### Component 3: Custom Errors

```go
// pkg/errors/errors.go
package errors

import "fmt"

// Base error types
type RetryableError struct {
	Msg string
	Err error
}

func (e *RetryableError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

type NonRetryableError struct {
	Msg string
	Err error
}

func (e *NonRetryableError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

// Specific error types
type ProviderTimeoutError struct {
	Provider string
	*RetryableError
}

func NewProviderTimeoutError(provider string) *ProviderTimeoutError {
	return &ProviderTimeoutError{
		Provider: provider,
		RetryableError: &RetryableError{
			Msg: fmt.Sprintf("provider %s timed out", provider),
		},
	}
}

type ProviderRateLimitError struct {
	Provider string
	*RetryableError
}

func NewProviderRateLimitError(provider string) *ProviderRateLimitError {
	return &ProviderRateLimitError{
		Provider: provider,
		RetryableError: &RetryableError{
			Msg: fmt.Sprintf("provider %s rate limited", provider),
		},
	}
}

type CircuitBreakerOpenError struct {
	Provider string
	*NonRetryableError
}

func NewCircuitBreakerOpenError(provider string) *CircuitBreakerOpenError {
	return &CircuitBreakerOpenError{
		Provider: provider,
		NonRetryableError: &NonRetryableError{
			Msg: fmt.Sprintf("circuit breaker open for provider %s", provider),
		},
	}
}

type ValidationError struct {
	Field string
	*NonRetryableError
}

func NewValidationError(field, msg string) *ValidationError {
	return &ValidationError{
		Field: field,
		NonRetryableError: &NonRetryableError{
			Msg: msg,
		},
	}
}

type MaxRetriesExceededError struct {
	RequestID string
	Attempts  int
	LastError error
}

func (e *MaxRetriesExceededError) Error() string {
	return fmt.Sprintf("request %s failed after %d attempts: %v", 
		e.RequestID, e.Attempts, e.LastError)
}
```

### Component 4: Request Validator

```go
// internal/validator/validator.go
package validator

import (
	"fmt"
	"strings"
	
	"github.com/yourusername/langdock-llm-reliability/internal/models"
	"github.com/yourusername/langdock-llm-reliability/pkg/errors"
)

type Validator struct {
	maxTokenLimits map[string]int
}

func NewValidator() *Validator {
	return &Validator{
		maxTokenLimits: map[string]int{
			"gpt-4":              8192,
			"gpt-4-32k":          32768,
			"gpt-3.5-turbo":      4096,
			"claude-3-opus":      200000,
			"claude-3-sonnet":    200000,
		},
	}
}

func (v *Validator) ValidateRequest(req *models.Request) error {
	// Check required fields
	if req.Model == "" {
		return errors.NewValidationError("model", "model is required")
	}
	
	if len(req.Messages) == 0 {
		return errors.NewValidationError("messages", "at least one message is required")
	}
	
	// Validate messages
	for i, msg := range req.Messages {
		if msg.Role == "" {
			return errors.NewValidationError(
				fmt.Sprintf("messages[%d].role", i),
				"role is required",
			)
		}
		if msg.Content == "" {
			return errors.NewValidationError(
				fmt.Sprintf("messages[%d].content", i),
				"content is required",
			)
		}
	}
	
	// Validate temperature
	if req.Temperature < 0 || req.Temperature > 2 {
		return errors.NewValidationError(
			"temperature",
			"temperature must be between 0 and 2",
		)
	}
	
	// Check token limits
	if req.MaxTokens > 0 {
		limit, exists := v.maxTokenLimits[req.Model]
		if exists && req.MaxTokens > limit {
			return errors.NewValidationError(
				"max_tokens",
				fmt.Sprintf("max_tokens exceeds limit of %d for model %s", 
					limit, req.Model),
			)
		}
	}
	
	// Estimate input tokens (simple approximation)
	totalChars := 0
	for _, msg := range req.Messages {
		totalChars += len(msg.Content)
	}
	estimatedTokens := totalChars / 4 // rough estimate: 1 token ≈ 4 chars
	
	limit, exists := v.maxTokenLimits[req.Model]
	if exists && estimatedTokens > limit {
		return errors.NewValidationError(
			"messages",
			fmt.Sprintf("estimated input tokens (%d) exceeds model limit (%d)",
				estimatedTokens, limit),
		)
	}
	
	return nil
}

func (v *Validator) CountTokens(messages []models.Message) int {
	// Simple token estimation
	totalChars := 0
	for _, msg := range messages {
		totalChars += len(msg.Content)
	}
	return totalChars / 4
}
```

### Component 5: Circuit Breaker

```go
// internal/client/circuit_breaker.go
package client

import (
	"sync"
	"time"
)

type CircuitState string

const (
	StateClosed   CircuitState = "closed"
	StateOpen     CircuitState = "open"
	StateHalfOpen CircuitState = "half_open"
)

type CircuitBreaker struct {
	name             string
	threshold        int
	timeout          time.Duration
	
	mu               sync.RWMutex
	state            CircuitState
	failureCount     int
	successCount     int
	lastFailureTime  time.Time
	openedAt         time.Time
}

func NewCircuitBreaker(name string, threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:      name,
		threshold: threshold,
		timeout:   timeout,
		state:     StateClosed,
	}
}

func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	if cb.state == StateOpen {
		// Check if timeout has passed
		if time.Since(cb.openedAt) > cb.timeout {
			cb.state = StateHalfOpen
			return false
		}
		return true
	}
	
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	cb.successCount++
	
	if cb.state == StateHalfOpen {
		// Success in half-open -> close circuit
		cb.state = StateClosed
		cb.failureCount = 0
	} else if cb.state == StateClosed {
		// Gradually decrease failure count
		if cb.failureCount > 0 {
			cb.failureCount--
		}
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	cb.failureCount++
	cb.lastFailureTime = time.Now()
	
	if cb.state == StateHalfOpen {
		// Failure in half-open -> reopen
		cb.state = StateOpen
		cb.openedAt = time.Now()
	} else if cb.state == StateClosed {
		// Check if should open
		if cb.failureCount >= cb.threshold {
			cb.state = StateOpen
			cb.openedAt = time.Now()
		}
	}
}

func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) GetFailureCount() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.failureCount
}
```

### Component 6: Provider Adapter Interface

```go
// internal/client/adapters/adapter.go
package adapters

import (
	"github.com/yourusername/langdock-llm-reliability/internal/models"
)

type ProviderAdapter interface {
	GetName() string
	GetEndpoint() string
	GetHeaders(apiKey string) map[string]string
	TransformRequest(req *models.Request) (map[string]interface{}, error)
	TransformResponse(resp map[string]interface{}) (*models.Response, error)
}

// internal/client/adapters/openai.go
package adapters

import (
	"fmt"
	"github.com/yourusername/langdock-llm-reliability/internal/models"
)

type OpenAIAdapter struct{}

func NewOpenAIAdapter() *OpenAIAdapter {
	return &OpenAIAdapter{}
}

func (a *OpenAIAdapter) GetName() string {
	return "openai"
}

func (a *OpenAIAdapter) GetEndpoint() string {
	return "https://api.openai.com/v1/chat/completions"
}

func (a *OpenAIAdapter) GetHeaders(apiKey string) map[string]string {
	return map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", apiKey),
		"Content-Type":  "application/json",
	}
}

func (a *OpenAIAdapter) TransformRequest(req *models.Request) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
	}
	
	if req.Temperature > 0 {
		result["temperature"] = req.Temperature
	}
	
	if req.MaxTokens > 0 {
		result["max_tokens"] = req.MaxTokens
	}
	
	if req.Stream {
		result["stream"] = true
	}
	
	return result, nil
}

func (a *OpenAIAdapter) TransformResponse(resp map[string]interface{}) (*models.Response, error) {
	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return nil, fmt.Errorf("invalid response format: no choices")
	}
	
	firstChoice := choices[0].(map[string]interface{})
	message := firstChoice["message"].(map[string]interface{})
	content := message["content"].(string)
	
	usage := resp["usage"].(map[string]interface{})
	totalTokens := int(usage["total_tokens"].(float64))
	
	return &models.Response{
		Content:    content,
		Model:      resp["model"].(string),
		TokensUsed: totalTokens,
	}, nil
}
```

### Component 7: HTTP Client with Retries

```go
// internal/client/client.go
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	
	"github.com/yourusername/langdock-llm-reliability/internal/config"
	"github.com/yourusername/langdock-llm-reliability/internal/models"
	"github.com/yourusername/langdock-llm-reliability/pkg/errors"
)

type HTTPClient struct {
	client          *http.Client
	circuitBreakers map[string]*CircuitBreaker
	config          *config.Config
}

func NewHTTPClient(cfg *config.Config) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: cfg.Timeout.Total,
		},
		circuitBreakers: make(map[string]*CircuitBreaker),
		config:          cfg,
	}
}

func (c *HTTPClient) SendRequest(
	ctx context.Context,
	provider string,
	url string,
	headers map[string]string,
	body map[string]interface{},
) (map[string]interface{}, error) {
	// Get or create circuit breaker
	cb := c.getCircuitBreaker(provider)
	
	// Check circuit breaker
	if cb.IsOpen() {
		return nil, errors.NewCircuitBreakerOpenError(provider)
	}
	
	// Marshal request body
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	
	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	
	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	
	// Send request
	startTime := time.Now()
	resp, err := c.client.Do(req)
	latency := time.Since(startTime)
	
	if err != nil {
		cb.RecordFailure()
		// Check if timeout
		if ctx.Err() == context.DeadlineExceeded {
			return nil, errors.NewProviderTimeoutError(provider)
		}
		return nil, &errors.RetryableError{Msg: "request failed", Err: err}
	}
	defer resp.Body.Close()
	
	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		cb.RecordFailure()
		return nil, &errors.RetryableError{Msg: "failed to read response", Err: err}
	}
	
	// Check status code
	if resp.StatusCode != http.StatusOK {
		cb.RecordFailure()
		
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, errors.NewProviderRateLimitError(provider)
		}
		
		if resp.StatusCode >= 500 {
			return nil, &errors.RetryableError{
				Msg: fmt.Sprintf("server error: %d", resp.StatusCode),
			}
		}
		
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, &errors.NonRetryableError{
				Msg: "authentication failed",
			}
		}
		
		return nil, &errors.NonRetryableError{
			Msg: fmt.Sprintf("request failed with status %d", resp.StatusCode),
		}
	}
	
	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		cb.RecordFailure()
		return nil, &errors.RetryableError{Msg: "failed to parse response", Err: err}
	}
	
	// Success!
	cb.RecordSuccess()
	
	return result, nil
}

func (c *HTTPClient) getCircuitBreaker(provider string) *CircuitBreaker {
	if cb, exists := c.circuitBreakers[provider]; exists {
		return cb
	}
	
	cb := NewCircuitBreaker(
		provider,
		c.config.CircuitBreaker.Threshold,
		c.config.CircuitBreaker.Timeout,
	)
	c.circuitBreakers[provider] = cb
	return cb
}
```

### Component 8: Provider Router

```go
// internal/router/router.go
package router

import (
	"github.com/yourusername/langdock-llm-reliability/internal/config"
	"github.com/yourusername/langdock-llm-reliability/internal/models"
)

type ProviderRouter struct {
	providers       []config.ProviderConfig
	modelToProvider map[string][]string
}

func NewProviderRouter(providers []config.ProviderConfig) *ProviderRouter {
	router := &ProviderRouter{
		providers:       providers,
		modelToProvider: make(map[string][]string),
	}
	
	// Build model -> providers mapping
	for _, provider := range providers {
		for _, model := range provider.Models {
			router.modelToProvider[model] = append(
				router.modelToProvider[model],
				provider.Name,
			)
		}
	}
	
	return router
}

func (r *ProviderRouter) SelectProvider(
	req *models.Request,
	attemptNumber int,
	previousProvider string,
) (string, error) {
	// Get eligible providers for this model
	eligible := r.modelToProvider[req.Model]
	if len(eligible) == 0 {
		return "", fmt.Errorf("no provider supports model %s", req.Model)
	}
	
	// First attempt: use preferred provider or first eligible
	if attemptNumber == 1 {
		if req.PreferredProvider != "" {
			for _, p := range eligible {
				if p == req.PreferredProvider {
					return p, nil
				}
			}
		}
		return eligible[0], nil
	}
	
	// Retry attempt: try different provider
	for _, p := range eligible {
		if p != previousProvider {
			return p, nil
		}
	}
	
	// No alternative, use same provider
	return eligible[0], nil
}

func (r *ProviderRouter) GetProviderConfig(name string) (*config.ProviderConfig, error) {
	for _, p := range r.providers {
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("provider %s not found", name)
}
```

### Component 9: Request Orchestrator (CORE)

```go
// internal/orchestrator/orchestrator.go
package orchestrator

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
	
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	
	"github.com/yourusername/langdock-llm-reliability/internal/client"
	"github.com/yourusername/langdock-llm-reliability/internal/client/adapters"
	"github.com/yourusername/langdock-llm-reliability/internal/config"
	"github.com/yourusername/langdock-llm-reliability/internal/models"
	"github.com/yourusername/langdock-llm-reliability/internal/router"
	"github.com/yourusername/langdock-llm-reliability/pkg/errors"
)

type Orchestrator struct {
	config   *config.Config
	client   *client.HTTPClient
	router   *router.ProviderRouter
	adapters map[string]adapters.ProviderAdapter
	logger   *logrus.Logger
}

func NewOrchestrator(
	cfg *config.Config,
	httpClient *client.HTTPClient,
	providerRouter *router.ProviderRouter,
	logger *logrus.Logger,
) *Orchestrator {
	// Initialize adapters
	adapterMap := map[string]adapters.ProviderAdapter{
		"openai":    adapters.NewOpenAIAdapter(),
		"anthropic": adapters.NewAnthropicAdapter(),
	}
	
	return &Orchestrator{
		config:   cfg,
		client:   httpClient,
		router:   providerRouter,
		adapters: adapterMap,
		logger:   logger,
	}
}

func (o *Orchestrator) ExecuteRequest(ctx context.Context, req *models.Request) (*models.Response, error) {
	// Generate request ID
	req.RequestID = o.generateRequestID()
	req.CreatedAt = time.Now()
	
	o.logger.WithFields(logrus.Fields{
		"request_id": req.RequestID,
		"model":      req.Model,
	}).Info("request started")
	
	var lastError error
	var lastProvider string
	attempts := make([]*models.Attempt, 0)
	
	for attemptNumber := 1; attemptNumber <= o.config.Retry.MaxRetries; attemptNumber++ {
		// Select provider
		provider, err := o.router.SelectProvider(req, attemptNumber, lastProvider)
		if err != nil {
			return nil, err
		}
		lastProvider = provider
		
		// Execute attempt
		attempt, response, err := o.executeAttempt(ctx, req, attemptNumber, provider)
		attempts = append(attempts, attempt)
		
		if err == nil {
			// Success!
			o.logger.WithFields(logrus.Fields{
				"request_id":     req.RequestID,
				"attempt_number": attemptNumber,
				"provider":       provider,
				"latency_ms":     attempt.LatencyMs,
			}).Info("request succeeded")
			
			response.RequestID = req.RequestID
			response.Attempts = attemptNumber
			response.Provider = provider
			return response, nil
		}
		
		lastError = err
		
		// Check if error is retryable
		if !o.isRetryable(err) {
			o.logger.WithFields(logrus.Fields{
				"request_id": req.RequestID,
				"error":      err.Error(),
			}).Error("non-retryable error")
			return nil, err
		}
		
		// Don't wait after last attempt
		if attemptNumber >= o.config.Retry.MaxRetries {
			break
		}
		
		// Calculate backoff
		waitTime := o.calculateBackoff(attemptNumber)
		
		o.logger.WithFields(logrus.Fields{
			"request_id":     req.RequestID,
			"attempt_number": attemptNumber,
			"provider":       provider,
			"error":          err.Error(),
			"wait_seconds":   waitTime.Seconds(),
		}).Warn("request retry scheduled")
		
		// Wait before retry
		select {
		case <-time.After(waitTime):
			// Continue to next attempt
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	
	// All retries exhausted
	return nil, &errors.MaxRetriesExceededError{
		RequestID: req.RequestID,
		Attempts:  len(attempts),
		LastError: lastError,
	}
}

func (o *Orchestrator) executeAttempt(
	ctx context.Context,
	req *models.Request,
	attemptNumber int,
	provider string,
) (*models.Attempt, *models.Response, error) {
	attempt := &models.Attempt{
		RequestID:     req.RequestID,
		AttemptNumber: attemptNumber,
		Provider:      provider,
		StartedAt:     time.Now(),
		Status:        "in_progress",
	}
	
	// Get provider config
	providerConfig, err := o.router.GetProviderConfig(provider)
	if err != nil {
		attempt.Status = "failed"
		attempt.ErrorType = "config_error"
		attempt.ErrorMessage = err.Error()
		return attempt, nil, err
	}
	
	// Get adapter
	adapter, exists := o.adapters[provider]
	if !exists {
		err := fmt.Errorf("no adapter for provider %s", provider)
		attempt.Status = "failed"
		attempt.ErrorType = "adapter_error"
		attempt.ErrorMessage = err.Error()
		return attempt, nil, err
	}
	
	// Transform request
	transformedReq, err := adapter.TransformRequest(req)
	if err != nil {
		attempt.Status = "failed"
		attempt.ErrorType = "transform_error"
		attempt.ErrorMessage = err.Error()
		return attempt, nil, err
	}
	
	// Send request
	respData, err := o.client.SendRequest(
		ctx,
		provider,
		adapter.GetEndpoint(),
		adapter.GetHeaders(providerConfig.APIKey),
		transformedReq,
	)
	
	endTime := time.Now()
	attempt.EndedAt = &endTime
	attempt.LatencyMs = endTime.Sub(attempt.StartedAt).Milliseconds()
	
	if err != nil {
		attempt.Status = "failed"
		attempt.ErrorType = fmt.Sprintf("%T", err)
		attempt.ErrorMessage = err.Error()
		return attempt, nil, err
	}
	
	// Transform response
	response, err := adapter.TransformResponse(respData)
	if err != nil {
		attempt.Status = "failed"
		attempt.ErrorType = "response_transform_error"
		attempt.ErrorMessage = err.Error()
		return attempt, nil, err
	}
	
	// Success
	attempt.Status = "success"
	attempt.TokensUsed = response.TokensUsed
	attempt.HTTPStatus = 200
	
	return attempt, response, nil
}

func (o *Orchestrator) calculateBackoff(attemptNumber int) time.Duration {
	// Exponential backoff: base * 2^(attempt-1)
	exp := math.Pow(2, float64(attemptNumber-1))
	backoff := float64(o.config.Retry.BaseBackoff) * exp
	
	// Add jitter (0-50% of backoff)
	jitter := backoff * rand.Float64() * 0.5
	total := backoff + jitter
	
	// Cap at max backoff
	if total > float64(o.config.Retry.MaxBackoff) {
		total = float64(o.config.Retry.MaxBackoff)
	}
	
	return time.Duration(total)
}

func (o *Orchestrator) isRetryable(err error) bool {
	switch err.(type) {
	case *errors.RetryableError:
		return true
	case *errors.ProviderTimeoutError:
		return true
	case *errors.ProviderRateLimitError:
		return true
	default:
		return false
	}
}

func (o *Orchestrator) generateRequestID() string {
	return fmt.Sprintf("req_%d_%s",
		time.Now().UnixMilli(),
		uuid.New().String()[:8],
	)
}
```

### Component 10: API Handlers

```go
// internal/api/handlers.go
package api

import (
	"net/http"
	
	"github.com/gin-gonic/gin"
	
	"github.com/yourusername/langdock-llm-reliability/internal/models"
	"github.com/yourusername/langdock-llm-reliability/internal/orchestrator"
	"github.com/yourusername/langdock-llm-reliability/internal/validator"
)

type Handler struct {
	orchestrator *orchestrator.Orchestrator
	validator    *validator.Validator
}

func NewHandler(orch *orchestrator.Orchestrator, val *validator.Validator) *Handler {
	return &Handler{
		orchestrator: orch,
		validator:    val,
	}
}

func (h *Handler) ChatCompletion(c *gin.Context) {
	var req models.Request
	
	// Parse request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request format",
			"details": err.Error(),
		})
		return
	}
	
	// Validate request
	if err := h.validator.ValidateRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "validation failed",
			"details": err.Error(),
		})
		return
	}
	
	// Execute request
	response, err := h.orchestrator.ExecuteRequest(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "request failed",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, response)
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}
```

### Component 11: Main Application

```go
// cmd/server/main.go
package main

import (
	"fmt"
	"log"
	
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	
	"github.com/yourusername/langdock-llm-reliability/internal/api"
	"github.com/yourusername/langdock-llm-reliability/internal/client"
	"github.com/yourusername/langdock-llm-reliability/internal/config"
	"github.com/yourusername/langdock-llm-reliability/internal/orchestrator"
	"github.com/yourusername/langdock-llm-reliability/internal/router"
	"github.com/yourusername/langdock-llm-reliability/internal/validator"
)

func main() {
	// Load configuration
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	// Setup logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	
	// Initialize components
	httpClient := client.NewHTTPClient(cfg)
	providerRouter := router.NewProviderRouter(cfg.Providers)
	val := validator.NewValidator()
	orch := orchestrator.NewOrchestrator(cfg, httpClient, providerRouter, logger)
	handler := api.NewHandler(orch, val)
	
	// Setup Gin router
	r := gin.Default()
	
	// Routes
	r.GET("/health", handler.Health)
	r.POST("/v1/chat/completions", handler.ChatCompletion)
	
	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logger.Infof("Starting server on %s", addr)
	
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
```

### Component 12: Mock Provider

```go
// cmd/mock-provider/main.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"
	
	"github.com/gin-gonic/gin"
)

type FailureConfig struct {
	TimeoutProbability       float64 `json:"timeout_probability"`
	RateLimitProbability     float64 `json:"rate_limit_probability"`
	ServerErrorProbability   float64 `json:"server_error_probability"`
	ResponseDelaySeconds     float64 `json:"response_delay_seconds"`
	TimeoutDurationSeconds   float64 `json:"timeout_duration_seconds"`
}

var failureConfig = FailureConfig{
	TimeoutDurationSeconds: 35.0,
}

func main() {
	r := gin.Default()
	
	// Mock OpenAI endpoint
	r.POST("/v1/chat/completions", handleChatCompletion)
	
	// Configuration endpoint
	r.POST("/config/failures", handleConfigFailures)
	
	log.Println("Mock provider starting on :8001")
	r.Run(":8001")
}

func handleChatCompletion(c *gin.Context) {
	// Simulate response delay
	if failureConfig.ResponseDelaySeconds > 0 {
		time.Sleep(time.Duration(failureConfig.ResponseDelaySeconds * float64(time.Second)))
	}
	
	// Simulate timeout
	if rand.Float64() < failureConfig.TimeoutProbability {
		time.Sleep(time.Duration(failureConfig.TimeoutDurationSeconds * float64(time.Second)))
		return
	}
	
	// Simulate rate limit
	if rand.Float64() < failureConfig.RateLimitProbability {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"message": "Rate limit exceeded",
				"type":    "rate_limit_error",
			},
		})
		return
	}
	
	// Simulate server error
	if rand.Float64() < failureConfig.ServerErrorProbability {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "Internal server error",
				"type":    "server_error",
			},
		})
		return
	}
	
	// Normal response
	c.JSON(http.StatusOK, gin.H{
		"id":      fmt.Sprintf("mock-%d", time.Now().Unix()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "gpt-4",
		"choices": []gin.H{
			{
				"index": 0,
				"message": gin.H{
					"role":    "assistant",
					"content": "This is a mock response from the LLM provider.",
				},
				"finish_reason": "stop",
			},
		},
		"usage": gin.H{
			"prompt_tokens":     10,
			"completion_tokens": 20,
			"total_tokens":      30,
		},
	})
}

func handleConfigFailures(c *gin.Context) {
	var config FailureConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	failureConfig = config
	
	c.JSON(http.StatusOK, gin.H{
		"status": "configured",
		"config": failureConfig,
	})
}
```

---

## Configuration File

```yaml
# config/config.yaml
server:
  host: "0.0.0.0"
  port: 8080

retry:
  max_retries: 3
  base_backoff: 2s
  max_backoff: 60s
  enable_failover: true

timeout:
  connect: 10s
  read: 30s
  total: 60s

circuit_breaker:
  threshold: 5
  timeout: 60s

streaming:
  heartbeat_timeout: 15s
  validate_chunks: true

providers:
  - name: "openai"
    base_url: "http://localhost:8001"  # Mock provider for testing
    api_key: "mock-key"
    models:
      - "gpt-4"
      - "gpt-3.5-turbo"
    priority: 1
    
  - name: "anthropic"
    base_url: "http://localhost:8001"  # Mock provider for testing
    api_key: "mock-key"
    models:
      - "claude-3-opus"
      - "claude-3-sonnet"
    priority: 2

observability:
  log_level: "info"
  metrics_port: 9090
```

---

## MVP Scope - Demo Focused

### What We MUST Build (for demo scenarios)

**Scenario 1: Normal Success**
- ✅ Basic request validation
- ✅ HTTP call to provider
- ✅ Request ID tracking
- ✅ Basic logging

**Scenario 2: Timeout → Retry**
- ✅ Timeout detection
- ✅ Retry loop (up to 3 attempts)
- ✅ Exponential backoff calculation
- ✅ Log showing backoff timing

**Scenario 3: Rate Limit → Failover**
- ✅ Detect 429 status
- ✅ Provider router (openai → anthropic)
- ✅ Simple provider adapters

**Scenario 4: Circuit Breaker**
- ✅ Failure counting per provider
- ✅ Circuit breaker state (closed/open)
- ✅ Skip provider when circuit open
- ✅ Log circuit breaker opening

**Scenario 5: Validation**
- ✅ Basic field validation (model, messages)
- ✅ Fast-fail (no API call)

**Supporting Infrastructure:**
- ✅ Mock provider with configurable failures
- ✅ Config file for settings
- ✅ Structured logging (JSON)

### What We Can SKIP (not needed for demo)

❌ Half-open circuit breaker state (just closed/open is fine)
❌ Advanced token counting (simple char estimation OK)
❌ Multiple retry strategies (just exponential backoff)
❌ Persistent circuit breaker state (in-memory is fine)
❌ Comprehensive error types (just basics)
❌ Unit tests (integration tests via demo are enough)
❌ Anthropic-specific format (mock can return same format)
❌ Request caching
❌ Metrics/monitoring
❌ Authentication

---

## Implementation Plan

Build in order of dependencies - foundation first, orchestration last.

### Phase 1: Foundation (Core Types & Utilities)

**Status:** In Progress

**Components:**
1. **Custom Error Types** (`pkg/errors/errors.go`)
   - Retryable vs Non-retryable errors
   - Specific error types: timeout, rate limit, validation, circuit breaker
   - Helper: `IsRetryable(err)` function

2. **Data Models** (`internal/models/`)
   - ✅ Request, Response, Message (already done)
   - ✅ Attempt tracking (already done)

3. **Configuration** (`internal/config/config.go`)
   - Load from YAML
   - Retry settings (max retries, backoff)
   - Timeout settings (connect, read, total)
   - Circuit breaker settings
   - Provider configurations

**Checkpoint:** Can define errors, models, and load config

---

### Phase 2: Validation & Safety (Fail Fast)

**Components:**
1. **Request Validator** (`internal/validator/validator.go`)
   - Validate required fields
   - Check parameter ranges (temperature, etc.)
   - Token counting and limit checking
   - Model name validation
   - Fast-fail on validation errors

**Tests:**
- Missing required fields
- Invalid temperature/parameters
- Token limit exceeded
- Valid requests pass

**Checkpoint:** Invalid requests fail immediately without API calls

---

### Phase 3: Resilience Primitives

**Components:**
1. **Circuit Breaker** (`internal/client/circuit_breaker.go`)
   - States: Closed, Open, Half-Open
   - Failure threshold tracking
   - Auto-recovery with timeout
   - Per-provider instances

2. **HTTP Client** (`internal/client/client.go`)
   - Timeout handling
   - Circuit breaker integration
   - Error classification (retryable vs not)
   - Response parsing

**Tests:**
- Circuit breaker opens after N failures
- Circuit breaker transitions to half-open
- Circuit breaker closes on success
- Timeout handling works

**Checkpoint:** Can make HTTP calls with circuit breaker protection

---

### Phase 4: Provider Abstraction

**Components:**
1. **Provider Adapter Interface** (`internal/client/adapters/adapter.go`)
   - Define common interface
   - Request transformation
   - Response transformation

2. **OpenAI Adapter** (`internal/client/adapters/openai.go`)
   - Transform to OpenAI format
   - Parse OpenAI responses

3. **Anthropic Adapter** (`internal/client/adapters/anthropic.go`)
   - Transform to Anthropic format
   - Parse Anthropic responses

4. **Provider Router** (`internal/router/router.go`)
   - Model → Provider mapping
   - Provider selection logic
   - Failover to alternative providers

**Tests:**
- Adapters transform requests correctly
- Router selects correct provider
- Router fails over on retry

**Checkpoint:** Can route requests to different providers

---

### Phase 5: Orchestration (The Brain)

**Components:**
1. **Orchestrator** (`internal/orchestrator/orchestrator.go`)
   - Main retry loop
   - Exponential backoff with jitter
   - Request ID generation
   - Attempt tracking
   - Integration of all components

**Logic Flow:**
```
1. Generate request ID
2. Validate request (validator)
3. Loop: up to max retries
   a. Select provider (router)
   b. Check circuit breaker
   c. Transform request (adapter)
   d. Send HTTP request (client)
   e. On failure: check if retryable
   f. If retryable: calculate backoff, retry
   g. If not retryable: fail immediately
4. Return response or error
```

**Tests:**
- Retries on timeout
- Retries on 5xx
- Fails immediately on 4xx
- Exponential backoff works
- Failover to different provider

**Checkpoint:** Full retry/failover logic working

---

### Phase 6: API Layer

**Components:**
1. **HTTP Handlers** (`internal/api/handlers.go`)
   - POST `/v1/chat/completions`
   - GET `/health`
   - Request parsing
   - Response formatting

2. **Main Application** (`cmd/server/main.go`)
   - Initialize all components
   - Wire dependencies
   - Start HTTP server

**Checkpoint:** Can receive HTTP requests and return responses

---

### Phase 7: Mock Provider (Testing)

**Components:**
1. **Mock LLM Server** (`cmd/mock-provider/main.go`)
   - Simulate successful responses
   - Configurable failure modes:
     - Timeout (sleep)
     - Rate limit (429)
     - Server error (500)
   - Configuration endpoint

**Checkpoint:** Can simulate all failure scenarios for testing

---

### Phase 8: Integration & Testing

**Tests:**
1. End-to-end success flow
2. Timeout with retry
3. Rate limit with retry
4. Circuit breaker trip and recovery
5. Provider failover
6. Validation failures

**Checkpoint:** All scenarios work end-to-end

---

## Simplified Build Order (Demo-Focused)

Build in this order for fastest path to working demo:

### Phase 1: Mock Provider First (2-3 hours)
**Why first?** You can test everything against it immediately.

1. ✅ Project structure (done)
2. **Build mock provider** (`cmd/mock-provider/main.go`)
   - Returns mock LLM responses
   - Configurable failures (timeout, 429, 500)
   - Configuration endpoint
   - **Test:** Can curl it and get responses

### Phase 2: Basic Flow (4-5 hours)
**Goal:** Scenario 1 (normal success) working

3. **Simple config** (`internal/config/config.go`)
   - Just hardcode or minimal YAML
   - Provider URLs, retry count, timeouts

4. **Basic models** (✅ mostly done)
   - Request, Response types

5. **Minimal validator** (`internal/validator/validator.go`)
   - Check model, messages not empty
   - That's it for now

6. **HTTP client** (`internal/client/client.go`)
   - Make POST request
   - Handle timeouts
   - Parse response

7. **API handler** (`internal/api/handlers.go`)
   - POST /v1/chat/completions
   - Parse request, call client, return response

8. **Wire it up** (`cmd/server/main.go`)
   - Start server
   - **Test:** Scenario 1 works!

### Phase 3: Retry Logic (3-4 hours)
**Goal:** Scenario 2 (timeout + retry) working

9. **Simple error types** (`pkg/errors/errors.go`)
   - Just: RetryableError, NonRetryableError

10. **Orchestrator with retry** (`internal/orchestrator/orchestrator.go`)
    - Retry loop (1, 2, 3 attempts)
    - Exponential backoff: 2s, 4s, 8s
    - Log each attempt
    - **Test:** Scenario 2 works!

### Phase 4: Failover (3-4 hours)
**Goal:** Scenario 3 (rate limit + failover) working

11. **Provider router** (`internal/router/router.go`)
    - Map: openai → anthropic on retry
    - Simple: attempt 1 = openai, attempt 2+ = anthropic

12. **Update orchestrator** for provider switching
    - **Test:** Scenario 3 works!

### Phase 5: Circuit Breaker (3-4 hours)
**Goal:** Scenario 4 (circuit breaker) working

13. **Simple circuit breaker** (`internal/client/circuit_breaker.go`)
    - Count failures per provider
    - If failures >= 5: state = open
    - If open: skip that provider
    - **Test:** Scenario 4 works!

### Phase 6: Polish (2-3 hours)

14. **Better logging**
    - JSON format
    - Request IDs
    - Attempt numbers

15. **Final testing**
    - Run all 5 scenarios
    - Fix any issues

16. **README**
    - How to run
    - Demo commands

---

**Total: ~18-20 hours**

## Absolute Minimum (if time is tight)

Can demo with just:
- Mock provider (2h)
- Basic request flow (3h)
- Retry logic (3h)
- Simple logging (1h)

= **9 hours** for 2 scenarios (normal + retry)

Then add failover (3h) and circuit breaker (3h) if time allows.

---

## Quick Start Commands

```bash
# Terminal 1: Start mock provider
cd cmd/mock-provider
go run main.go

# Terminal 2: Start main application
cd cmd/server
go run main.go

# Terminal 3: Test requests
# Normal request
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'

# Configure mock to timeout
curl -X POST http://localhost:8001/config/failures \
  -H "Content-Type: application/json" \
  -d '{
    "timeout_probability": 1.0
  }'

# Send request (should retry)
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

---

## Testing Scenarios

```bash
# Scenario 1: Normal Success
curl -X POST http://localhost:8001/config/failures -d '{}'
curl -X POST http://localhost:8080/v1/chat/completions -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}'

# Scenario 2: Timeout with Retry
curl -X POST http://localhost:8001/config/failures -d '{"timeout_probability":0.5}'
# Run multiple times, observe retries in logs

# Scenario 3: Rate Limit with Failover
curl -X POST http://localhost:8001/config/failures -d '{"rate_limit_probability":1.0}'
# Should failover to anthropic provider

# Scenario 4: Circuit Breaker
curl -X POST http://localhost:8001/config/failures -d '{"server_error_probability":1.0}'
# Send 10 requests, circuit breaker should open

# Scenario 5: Circuit Breaker Recovery
# Wait 60 seconds, circuit breaker should go to half-open
curl -X POST http://localhost:8001/config/failures -d '{}'
# Next request should succeed and close circuit
```

---

## Demo Script for Interview

### Part 1: Architecture Overview (2 minutes)
Show the architecture diagram and explain the flow:
- Request comes in → Validator → Orchestrator → Router → HTTP Client → Provider
- Cross-cutting: Circuit Breaker, Retry Logic, Observability

### Part 2: Code Walkthrough (3 minutes)
Walk through key components:
1. **Orchestrator** - Show retry loop and backoff calculation
2. **Circuit Breaker** - Explain state transitions
3. **Provider Router** - Show failover logic

### Part 3: Live Demo (4 minutes)

```bash
# Demo 1: Successful Request
echo "=== Demo 1: Normal Request ==="
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello!"}]}'

# Demo 2: Retry on Timeout
echo -e "\n=== Demo 2: Timeout with Retry ==="
curl -X POST http://localhost:8001/config/failures \
  -d '{"timeout_probability":0.7}'
  
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Test"}]}'
# Show logs demonstrating retry

# Demo 3: Provider Failover
echo -e "\n=== Demo 3: Provider Failover ==="
curl -X POST http://localhost:8001/config/failures \
  -d '{"rate_limit_probability":1.0}'
  
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Test"}]}'
# Show logs demonstrating failover to anthropic

# Demo 4: Circuit Breaker
echo -e "\n=== Demo 4: Circuit Breaker ==="
curl -X POST http://localhost:8001/config/failures \
  -d '{"server_error_probability":1.0}'
  
# Send multiple requests to trigger circuit breaker
for i in {1..6}; do
  echo "Request $i"
  curl -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -d '{"model":"gpt-4","messages":[{"role":"user","content":"Test"}]}' &
done
wait
# Show circuit breaker open in logs
```

### Part 4: Show Logs (1 minute)
Point out key log entries:
- Request ID tracking across attempts
- Retry backoff calculations
- Circuit breaker state changes
- Provider failover events

---

## Key Trade-offs to Discuss

Be ready to discuss:

1. **Why Go over Python?**
   - Better concurrency model (goroutines vs async/await)
   - Single binary deployment
   - Better performance for I/O-heavy workloads
   - Strong typing catches errors at compile time

2. **Exponential Backoff with Jitter**
   - Prevents thundering herd
   - Alternative: Fixed delays (simpler but less effective)

3. **Circuit Breaker per Provider**
   - Isolates failing providers
   - Alternative: Global circuit breaker (simpler but less granular)

4. **Automatic Failover**
   - Better availability
   - Trade-off: Higher cost (multiple provider calls)

5. **Request ID Tracking**
   - Single ID for entire request lifecycle
   - Makes debugging straightforward

---

## MVP Scope (What's Included)

✅ **Included:**
- Request validation
- Retry logic with exponential backoff
- Circuit breaker per provider
- Provider failover
- Timeout handling
- Request tracking with IDs
- Structured logging
- Mock provider for testing
- Integration tests

❌ **Not Included (Production Would Need):**
- Streaming support (out of scope for 18h)
- Persistent storage (Redis/Postgres)
- Advanced metrics (Prometheus)
- Distributed tracing (OpenTelemetry)
- Rate limiting per customer
- Authentication/authorization
- Production-ready error handling for all edge cases
- Comprehensive test coverage

---

## Success Criteria

By Monday 09:30, you should have:

1. ✅ Working Go application that starts and serves requests
2. ✅ Request validation that fails fast on invalid inputs
3. ✅ Retry logic that handles timeouts with exponential backoff
4. ✅ Circuit breaker that prevents cascading failures
5. ✅ Provider failover that switches providers on failure
6. ✅ Request tracking with unique IDs through all attempts
7. ✅ Structured logs showing the entire request journey
8. ✅ Mock provider that can simulate various failure modes
9. ✅ Working demos of all failure scenarios
10. ✅ Clean, presentable code ready to discuss

---

## Tips for the Hackathon

1. **Stay Focused:** Don't add features beyond the core MVP
2. **Test as You Go:** Write tests immediately after each component
3. **Use the Mock Provider:** It will save hours of debugging
4. **Log Everything:** Logs are your demo and debugging tool
5. **Keep It Simple:** Use in-memory state, no databases needed
6. **Commit Often:** Git commit after each working component
7. **Take Breaks:** 10 min break every 90 minutes
8. **Sleep Well:** Better to be rested than exhausted for demo

---

## Emergency Fallback Plan

If running behind schedule (check at 22:00 Sunday):

**Minimum Viable Demo:**
1. Request validation ✅
2. Basic retry (without circuit breaker) ✅
3. Simple logging ✅
4. One working demo scenario ✅

This is still impressive and shows solid thinking!

---

## Questions to Prepare For

1. **"How would this scale to 1000 req/s?"**
   - Answer: Go's goroutines handle concurrency well. Would need connection pooling, request queuing, worker pools.

2. **"How do you handle streaming responses?"**
   - Answer: Not in MVP, but would use SSE parsing, heartbeat monitoring, chunk validation. Describe design from blueprint.

3. **"What about cost optimization?"**
   - Answer: Circuit breakers prevent wasted calls. Could add request caching, smart provider selection by cost.

4. **"How would you make this production-ready?"**
   - Answer: Add Redis for state, Postgres for audit logs, Prometheus for metrics, OpenTelemetry for tracing, comprehensive tests.

5. **"Why not use an existing library?"**
   - Answer: Existing retry libraries don't handle multi-provider failover or circuit breakers with our specific requirements.

---

Good luck with your hackathon! You've got a solid plan and all the code you need. Stay focused, test as you go, and you'll have an impressive demo by Monday morning! 🚀
