package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/identity/domain"
	"offerlog/backend/internal/jdparse"
	"offerlog/backend/internal/platform/httpx"
)

type stubParser struct {
	result jdparse.Result
	err    error
	calls  int
}

func (s *stubParser) Parse(_ context.Context, _ string) (jdparse.Result, error) {
	s.calls++
	return s.result, s.err
}

func newParseURLServer(t *testing.T, h *parseURLHandler) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &domain.User{ID: 7, Email: "x@y.z", Timezone: "Europe/Dublin", Locale: "zh-CN"})
		c.Next()
	})
	h.parseURLRoute(r.Group("/api/v1"))
	return httptest.NewServer(r)
}

func postJSON(t *testing.T, srv *httptest.Server, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/applications/parse-url", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	return res, out
}

func TestParseURLSuccessReturnsFields(t *testing.T) {
	stub := &stubParser{result: jdparse.Result{CompanyName: "字节跳动", Position: "前端工程师", Location: "上海", Confidence: 0.9}}
	h := newParseURLHandlerWith(stub, 10)
	srv := newParseURLServer(t, h)
	defer srv.Close()

	res, out := postJSON(t, srv, `{"url":"https://jobs.example.com/x"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if out["company_name"] != "字节跳动" || out["position"] != "前端工程师" || out["location"] != "上海" {
		t.Fatalf("unexpected parse result %v", out)
	}
}

func TestParseURLErrorDegradesToEmptyFields(t *testing.T) {
	stub := &stubParser{err: context.DeadlineExceeded}
	h := newParseURLHandlerWith(stub, 10)
	srv := newParseURLServer(t, h)
	defer srv.Close()

	res, out := postJSON(t, srv, `{"url":"https://jobs.example.com/x"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("degraded parse must be 200, got %d", res.StatusCode)
	}
	for _, k := range []string{"company_name", "position", "location"} {
		if v, ok := out[k].(string); !ok || v != "" {
			t.Errorf("field %s = %#v, want empty string", k, out[k])
		}
	}
}

func TestParseURLRejectsNonHTTPAndBlank(t *testing.T) {
	stub := &stubParser{}
	h := newParseURLHandlerWith(stub, 10)
	srv := newParseURLServer(t, h)
	defer srv.Close()

	for _, body := range []string{
		`{"url":""}`,
		`{"url":"file:///etc/passwd"}`,
		`{"url":"not-a-url"}`,
		`{}`,
	} {
		res, _ := postJSON(t, srv, body)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("body %s → %d, want 400", body, res.StatusCode)
		}
	}
	if stub.calls != 0 {
		t.Error("invalid input must never reach the parser")
	}
}

func TestParseURLRateLimit(t *testing.T) {
	stub := &stubParser{}
	h := newParseURLHandlerWith(stub, 3)
	srv := newParseURLServer(t, h)
	defer srv.Close()

	statuses := map[int]int{}
	for i := 0; i < 5; i++ {
		res, _ := postJSON(t, srv, `{"url":"https://jobs.example.com/x"}`)
		statuses[res.StatusCode]++
	}
	if statuses[http.StatusOK] != 3 || statuses[http.StatusTooManyRequests] != 2 {
		t.Fatalf("after 5 calls want 3×200 + 2×429, got %v", statuses)
	}
}
