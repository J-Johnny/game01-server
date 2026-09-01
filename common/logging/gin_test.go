package logging

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGinAccessSetsAndLogsRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	router := gin.New()
	router.Use(GinAccess(logger))
	router.GET("/healthz", func(c *gin.Context) {
		if RequestID(c) == "" {
			t.Fatal("request ID is missing from context")
		}
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if response.Header().Get(requestIDHeader) == "" {
		t.Fatal("request ID response header is missing")
	}
	if !bytes.Contains(output.Bytes(), []byte(`"path":"/healthz"`)) {
		t.Fatalf("access log = %s", output.String())
	}
}
