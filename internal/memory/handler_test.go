package memory

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerRejectsForeignOrgMemoryScope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(&fakeRepo{})).Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/v1/memory", strings.NewReader(`{
		"kind":"user_preference",
		"scope_type":"org",
		"scope_id":"org-b",
		"key":"timezone",
		"content_text":"UTC"
	}`))
	req.Header.Set("X-Org-ID", "org-a")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerAllowsOwnOrgMemoryScope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewHandler(NewUsecases(&fakeRepo{})).Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/v1/memory", strings.NewReader(`{
		"kind":"user_preference",
		"scope_type":"org",
		"scope_id":"org-a",
		"key":"timezone",
		"content_text":"UTC"
	}`))
	req.Header.Set("X-Org-ID", "org-a")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
