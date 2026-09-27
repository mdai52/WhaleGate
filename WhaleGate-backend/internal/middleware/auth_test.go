package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	whalegatejwt "github.com/whalegate/whalegate/internal/pkg/jwt"
)

func TestExtractAPIKey(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(r *http.Request)
		expected string
	}{
		{"Authorization Bearer", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer sk-abc")
		}, "sk-abc"},
		{"X-Api-Key 优先", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer sk-from-auth")
			r.Header.Set("X-Api-Key", "sk-from-header")
		}, "sk-from-header"},
		{"查询参数兜底", func(r *http.Request) {
			r.Header.Set("Authorization", "")
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models?api_key=sk-query", nil)
			c.setup(ctx.Request)

			got := extractAPIKey(ctx)
			if c.name == "查询参数兜底" && got != "sk-query" {
				t.Fatalf("期望 sk-query，实际 %q", got)
			}
			if c.name != "查询参数兜底" && got != c.expected {
				t.Fatalf("期望 %q，实际 %q", c.expected, got)
			}
		})
	}
}

func TestJWTAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jm := whalegatejwt.New("secret", "whalegate", time.Hour)
	token, err := jm.Generate(whalegatejwt.Claims{UserID: 42, Role: "admin"})
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	t.Run("缺少令牌", func(t *testing.T) {
		router := gin.New()
		router.Use(JWTAuth(jm))
		router.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望 401，实际 %d", w.Code)
		}
	})

	t.Run("有效令牌", func(t *testing.T) {
		router := gin.New()
		router.Use(JWTAuth(jm))
		router.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200，实际 %d", w.Code)
		}
	})

	t.Run("无效令牌", func(t *testing.T) {
		router := gin.New()
		router.Use(JWTAuth(jm))
		router.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer bad-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望 401，实际 %d", w.Code)
		}
	})
}

func TestRequireRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jm := whalegatejwt.New("secret", "whalegate", time.Hour)
	userToken, _ := jm.Generate(whalegatejwt.Claims{UserID: 1, Role: "user"})

	router := gin.New()
	router.Use(JWTAuth(jm), RequireRole("admin"))
	router.GET("/admin", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("期望 403，实际 %d", w.Code)
	}
}
