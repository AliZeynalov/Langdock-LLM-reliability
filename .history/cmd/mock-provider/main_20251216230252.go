package main

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func main() {
	// Configure logging
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	// Get port from args or default to 8001
	port := "8001"
	if len(port) == 0 {
		port = "8001"
	}

	r := gin.New()
	r.Use(gin.Recovery())

	// Chat completions endpoint (OpenAI-compatible)
	r.POST("/v1/chat/completions", handleChatCompletion)

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	log.Infof("Mock LLM Provider starting on :%s", port)
	r.Run(":" + port)
}

func handleChatCompletion(c *gin.Context) {
	// Get failure simulation params from query string
	delayStr := c.Query("delay")
	fail := c.Query("fail")
	stream := c.Query("stream")
	failChunkStr := c.Query("fail_chunk")

	log.WithFields(log.Fields{
		"delay":      delayStr,
		"fail":       fail,
		"stream":     stream,
		"fail_chunk": failChunkStr,
	}).Info("Received request")

	// Apply delay if specified
	if delayStr != "" {
		ms, err := strconv.Atoi(delayStr)
		if err == nil && ms > 0 {
			log.Infof("Applying delay of %dms", ms)
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
	}

	// Simulate failures
	if fail != "" {
		handleFailure(c, fail)
		return
	}

	// Handle streaming vs non-streaming
	if stream == "true" {
		failChunk := -1
		if failChunkStr != "" {
			failChunk, _ = strconv.Atoi(failChunkStr)
		}
		handleStreaming(c, failChunk)
	} else {
		handleNormalResponse(c)
	}
}

func handleFailure(c *gin.Context, failType string) {
	log.Warnf("Simulating failure: %s", failType)

	switch failType {
	case "429":
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"message": "Rate limit exceeded. Please retry after some time.",
				"type":    "rate_limit_error",
				"code":    "rate_limit_exceeded",
			},
		})
	case "500":
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "Internal server error",
				"type":    "server_error",
				"code":    "internal_error",
			},
		})
	case "502":
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "Bad gateway",
				"type":    "server_error",
				"code":    "bad_gateway",
			},
		})
	case "503":
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"message": "Service temporarily unavailable",
				"type":    "server_error",
				"code":    "service_unavailable",
			},
		})
	case "timeout":
		// Sleep for 60 seconds to simulate timeout
		log.Info("Simulating timeout (sleeping 60s)")
		time.Sleep(60 * time.Second)
		c.JSON(http.StatusGatewayTimeout, gin.H{
			"error": gin.H{
				"message": "Gateway timeout",
				"type":    "timeout_error",
				"code":    "timeout",
			},
		})
	default:
		// Try to parse as status code
		code, err := strconv.Atoi(failType)
		if err == nil && code >= 400 && code < 600 {
			c.JSON(code, gin.H{
				"error": gin.H{
					"message": fmt.Sprintf("Simulated error %d", code),
					"type":    "simulated_error",
					"code":    fmt.Sprintf("error_%d", code),
				},
			})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"message": "Unknown failure type",
					"type":    "server_error",
				},
			})
		}
	}
}

func handleNormalResponse(c *gin.Context) {
	log.Info("Returning normal response")

	response := gin.H{
		"id":      fmt.Sprintf("mock-%d", rand.Intn(100000)),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "gpt-4",
		"choices": []gin.H{
			{
				"index": 0,
				"message": gin.H{
					"role":    "assistant",
					"content": "Hello! I'm a mock LLM response. How can I help you today?",
				},
				"finish_reason": "stop",
			},
		},
		"usage": gin.H{
			"prompt_tokens":     10,
			"completion_tokens": 15,
			"total_tokens":      25,
		},
	}

	c.JSON(http.StatusOK, response)
}

func handleStreaming(c *gin.Context, failChunk int) {
	log.WithField("fail_chunk", failChunk).Info("Starting streaming response")

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	// Chunks to send
	chunks := []string{
		"Hello",
		" from",
		" the",
		" streaming",
		" mock",
		" provider",
		"!",
	}

	c.Stream(func(w io.Writer) bool {
		for i, chunk := range chunks {
			chunkNum := i + 1

			// Check if we should fail at this chunk
			if failChunk > 0 && chunkNum == failChunk {
				log.Warnf("Simulating failure at chunk %d", chunkNum)
				// Send malformed JSON to simulate mid-stream failure
				fmt.Fprintf(w, "data: {\"id\":\"mock-%d\",\"choices\":[{\"delta\":{\"content\":\n\n", chunkNum)
				c.Writer.Flush()
				return false
			}

			// Send valid chunk
			data := fmt.Sprintf(`{"id":"mock-%d","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"%s"},"finish_reason":null}]}`, chunkNum, chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			c.Writer.Flush()

			log.WithFields(log.Fields{
				"chunk": chunkNum,
				"text":  chunk,
			}).Debug("Sent chunk")

			// Simulate generation delay
			time.Sleep(100 * time.Millisecond)
		}

		// Send final chunk with finish_reason
		finalData := `{"id":"mock-final","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
		fmt.Fprintf(w, "data: %s\n\n", finalData)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		c.Writer.Flush()

		log.Info("Streaming complete")
		return false
	})
}

