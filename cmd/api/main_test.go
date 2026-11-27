package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/middleware"
)

func TestCORSAndTrustedProxyRateLimit(t *testing.T) {
	t.Setenv("AUTH_RATE_LIMIT", "2")
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{App: config.AppConfig{AllowedOrigins: []string{"https://portal.example.com"}, TrustedProxies: []string{"172.30.0.2"}}}
	router, err := initRouter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	router.GET("/login", middleware.RateLimitMiddleware(), func(c *gin.Context) { c.String(200, c.ClientIP()) })
	for i, spoof := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
		req := httptest.NewRequest("GET", "/login", nil)
		req.RemoteAddr = "198.51.100.21:1234"
		req.Header.Set("X-Forwarded-For", spoof)
		req.Header.Set("Origin", "https://portal.example.com")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		want := 200
		if i == 2 {
			want = 429
		}
		if res.Code != want || res.Header().Get("Access-Control-Allow-Origin") != "https://portal.example.com" {
			t.Fatalf("spoofed request bypassed rate limit or CORS: %d %s", res.Code, res.Body)
		}
	}
	req := httptest.NewRequest("GET", "/login", nil)
	req.RemoteAddr = "172.30.0.2:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Body.String() != "203.0.113.99" {
		t.Fatal("trusted proxy client IP was lost")
	}
	req = httptest.NewRequest("OPTIONS", "/login", nil)
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatal("unapproved CORS origin accepted")
	}
}

func TestShutdownDrainsInFlightRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	address := make(chan string, 1)
	entered, release := make(chan struct{}), make(chan struct{})
	server := &http.Server{Addr: "127.0.0.1:0", BaseContext: func(l net.Listener) context.Context {
		address <- l.Addr().String()
		return context.Background()
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte("completed"))
	})}
	defer server.Close()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, server) }()
	var addr string
	select {
	case addr = <-address:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}
	response := make(chan string, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		res, err := client.Get("http://" + addr)
		if err != nil {
			response <- err.Error()
			return
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		response <- string(body)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not arrive")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("shutdown dropped active request: %v", err)
	default:
	}
	close(release)
	if body := <-response; body != "completed" {
		t.Fatalf("request was interrupted: %s", body)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
}
