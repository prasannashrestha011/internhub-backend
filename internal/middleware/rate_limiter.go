package middleware

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Simple fixed-window rate limiter per client IP.
// Configurable via environment variables:
// AUTH_RATE_LIMIT - requests per window (default 10)
// AUTH_RATE_WINDOW - window in seconds (default 60)

type clientInfo struct {
	Requests    int
	WindowStart time.Time
}

var (
	clients   = make(map[string]*clientInfo)
	clientsMu sync.Mutex
	// cleanup interval
	cleanupOnce sync.Once
)

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func RateLimitMiddleware() gin.HandlerFunc {
	limit := getEnvInt("AUTH_RATE_LIMIT", 10)
	windowSec := getEnvInt("AUTH_RATE_WINDOW", 60)
	window := time.Duration(windowSec) * time.Second

	// start cleanup goroutine once
	cleanupOnce.Do(func() {
		go func() {
			for {
				time.Sleep(window * 2)
				clientsMu.Lock()
				now := time.Now()
				for ip, info := range clients {
					if now.Sub(info.WindowStart) > window*2 {
						delete(clients, ip)
					}
				}
				clientsMu.Unlock()
			}
		}()
	})

	return func(c *gin.Context) {
		ip := c.ClientIP()
		clientsMu.Lock()
		info, ok := clients[ip]
		if !ok {
			info = &clientInfo{Requests: 0, WindowStart: time.Now()}
			clients[ip] = info
		}
		// reset window if expired
		if time.Since(info.WindowStart) > window {
			info.Requests = 0
			info.WindowStart = time.Now()
		}
		info.Requests++
		reqs := info.Requests
		clientsMu.Unlock()

		if reqs > limit {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"success": false, "message": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}
