package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/AliZeynalov/LangDock-LLM-reliability/internal/models"
	"github.com/AliZeynalov/LangDock-LLM-reliability/internal/provider"
	"github.com/AliZeynalov/LangDock-LLM-reliability/internal/validator"
)

// Handler handles HTTP requests for the gateway
type Handler struct {
	providerClient *provider.Client
}

// NewHandler creates a new Handler
func NewHandler(client *provider.Client) *Handler {
	return &Handler{providerClient: client}
}

// ChatCompletion handles POST /v1/chat/completions
func (h *Handler) ChatCompletion(c *gin.Context) {
	requestID := c.GetString("request_id")
	start := time.Now()

	// Parse request body
	var req models.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		log.WithFields(log.Fields{
			"request_id": requestID,
			"error":      err.Error(),
			"event":      "parse_error",
		}).Warn("Failed to parse request body")

		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "Failed to parse request body: " + err.Error(),
			},
		})
		return
	}

	// Validate request
	if err := validator.ValidateRequest(&req); err != nil {
		log.WithFields(log.Fields{
			"request_id": requestID,
			"error":      err.Error(),
			"event":      "validation_failed",
		}).Warn("Request validation failed")

		// Return detailed validation errors
		if validErrs, ok := err.(*validator.ValidationErrors); ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"type":    "validation_error",
					"message": "Request validation failed",
					"details": validErrs.Errors,
				},
			})
			return
		}

		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "validation_error",
				"message": err.Error(),
			},
		})
		return
	}

	log.WithFields(log.Fields{
		"request_id": requestID,
		"model":      req.Model,
		"event":      "validated",
	}).Info("Request validated")

	// Create context with request ID
	ctx := context.WithValue(c.Request.Context(), "request_id", requestID)

	// Check if streaming is requested
	if req.Stream {
		h.handleStreamingRequest(c, ctx, &req, requestID, start)
		return
	}

	// Non-streaming request
	h.handleNonStreamingRequest(c, ctx, &req, requestID, start)
}

func (h *Handler) handleNonStreamingRequest(c *gin.Context, ctx context.Context, req *models.Request, requestID string, start time.Time) {
	// Call provider
	response, err := h.providerClient.Call(ctx, *req)
	if err != nil {
		log.WithFields(log.Fields{
			"request_id": requestID,
			"error":      err.Error(),
			"event":      "provider_error",
		}).Error("Provider call failed")

		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"type":    "provider_error",
				"message": "Provider error: " + err.Error(),
			},
		})
		return
	}

	// Build response
	result := models.Response{
		RequestID:      requestID,
		Content:        response.Content,
		Model:          req.Model,
		Provider:       response.Provider,
		Attempts:       response.Attempts,
		TotalLatencyMs: time.Since(start).Milliseconds(),
		CreatedAt:      time.Now(),
	}

	log.WithFields(log.Fields{
		"request_id": requestID,
		"provider":   response.Provider,
		"attempts":   response.Attempts,
		"latency_ms": result.TotalLatencyMs,
		"event":      "success",
	}).Info("Request successful")

	c.JSON(http.StatusOK, result)
}

func (h *Handler) handleStreamingRequest(c *gin.Context, ctx context.Context, req *models.Request, requestID string, start time.Time) {
	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Request-ID", requestID)

	// Call provider with streaming
	err := h.providerClient.CallStream(ctx, *req, c.Writer)
	if err != nil {
		log.WithFields(log.Fields{
			"request_id": requestID,
			"error":      err.Error(),
			"event":      "stream_error",
		}).Error("Streaming failed")
		// Note: Can't send JSON error after streaming has started
		// The error handling is done within CallStream
		return
	}

	log.WithFields(log.Fields{
		"request_id": requestID,
		"latency_ms": time.Since(start).Milliseconds(),
		"event":      "stream_complete",
	}).Info("Streaming complete")
}

// Health handles GET /health
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

