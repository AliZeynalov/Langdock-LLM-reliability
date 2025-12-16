package gateway

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// RequestIDMiddleware generates a unique ID for each request
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Generate unique ID: "req_a1b2c3d4"
		requestID := "req_" + uuid.New().String()[:8]

		// Store in Gin context (accessible throughout request lifecycle)
		c.Set("request_id", requestID)

		// Return in response header for client debugging
		c.Header("X-Request-ID", requestID)

		// Continue to next middleware/handler
		c.Next()
	}
}

// LoggingMiddleware logs request start/end with timing
func LoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := c.GetString("request_id")

		log.WithFields(log.Fields{
			"request_id": requestID,
			"method":     c.Request.Method,
			"path":       c.Request.URL.Path,
			"event":      "started",
		}).Info("Request started")

		// Process request
		c.Next()

		// Log completion
		log.WithFields(log.Fields{
			"request_id": requestID,
			"status":     c.Writer.Status(),
			"latency_ms": time.Since(start).Milliseconds(),
			"event":      "completed",
		}).Info("Request completed")
	}
}

