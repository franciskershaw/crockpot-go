package middleware_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

const gzipTestOrigin = "http://localhost:5173"

var gzipTestPayload = gin.H{"items": []string{"zz-sentinel-item-1", "zz-sentinel-item-2", "zz-sentinel-item-3"}}

const gzipTestPayloadJSON = `{"items":["zz-sentinel-item-1","zz-sentinel-item-2","zz-sentinel-item-3"]}`

// newGzipRouter mirrors main.go's global chain: CORS, then Gzip, then the rate limiter.
func newGzipRouter(rate limiter.Rate) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.CORS(gzipTestOrigin))
	r.Use(middleware.Gzip())
	r.Use(middleware.NewRateLimitMiddleware(memory.NewStore(), rate).Handler())
	r.GET("/items", func(c *gin.Context) {
		c.JSON(http.StatusOK, gzipTestPayload)
	})
	r.GET("/bad", func(c *gin.Context) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
	})
	r.GET("/private", middleware.AuthMiddleware("test-secret"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gzipTestPayload)
	})
	return r
}

func doGzipGet(r *gin.Engine, path string, acceptGzip bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Origin", gzipTestOrigin)
	if acceptGzip {
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

var generousRate = limiter.Rate{Period: time.Minute, Limit: 100}

func TestGzip_CompressesWhenClientAcceptsGzip(t *testing.T) {
	w := doGzipGet(newGzipRouter(generousRate), "/items", true)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding=gzip, got %q", got)
	}
	vary := w.Header().Values("Vary")
	if !slices.Contains(vary, "Accept-Encoding") {
		t.Errorf("expected Vary to include Accept-Encoding, got %q", vary)
	}
	if !slices.Contains(vary, "Origin") {
		t.Errorf("expected Vary to keep CORS's Origin, got %q", vary)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != gzipTestOrigin {
		t.Errorf("expected Access-Control-Allow-Origin=%s, got %q", gzipTestOrigin, got)
	}

	gz, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("body is not valid gzip: %v", err)
	}
	decoded, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if string(decoded) != gzipTestPayloadJSON {
		t.Errorf("decompressed body = %s, want %s", decoded, gzipTestPayloadJSON)
	}
}

func TestGzip_LeavesBodyPlainWithoutAcceptEncoding(t *testing.T) {
	w := doGzipGet(newGzipRouter(generousRate), "/items", false)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("expected no Content-Encoding, got %q", got)
	}
	if got := w.Body.String(); got != gzipTestPayloadJSON {
		t.Errorf("body = %s, want %s", got, gzipTestPayloadJSON)
	}
}

func TestGzip_ErrorResponsesReachClientIntact(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		rate     limiter.Rate
		wantCode int
		wantBody string
	}{
		{"400 from handler", "/bad", generousRate, http.StatusBadRequest, `{"error":"invalid_request"}`},
		{"401 from auth middleware", "/private", generousRate, http.StatusUnauthorized, `{"error":"unauthorized"}`},
		{"429 from rate limiter", "/items", limiter.Rate{Period: time.Minute, Limit: 1}, http.StatusTooManyRequests, `{"error":"rate_limit_exceeded"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newGzipRouter(tt.rate)
			w := doGzipGet(r, tt.path, true)
			if tt.wantCode == http.StatusTooManyRequests {
				w = doGzipGet(r, tt.path, true)
			}

			if w.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d", tt.wantCode, w.Code)
			}
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != gzipTestOrigin {
				t.Errorf("expected Access-Control-Allow-Origin=%s, got %q", gzipTestOrigin, got)
			}
			if got := w.Header().Get("Content-Encoding"); got != "" {
				t.Errorf("expected no Content-Encoding on an error, got %q", got)
			}
			if got := strings.TrimSpace(w.Body.String()); got != tt.wantBody {
				t.Errorf("body = %s, want %s", got, tt.wantBody)
			}
			if tt.wantCode == http.StatusTooManyRequests && w.Header().Get("Retry-After") == "" {
				t.Error("expected Retry-After on a 429")
			}
		})
	}
}
