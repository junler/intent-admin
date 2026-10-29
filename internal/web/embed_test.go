package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestUnbuiltFrontendServesPlaceholder(t *testing.T) {
	w := httptest.NewRecorder()
	serveIndex(w, httptest.NewRequest("GET", "/", nil), fstest.MapFS{})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "前端尚未构建") {
		t.Fatalf("placeholder response = %d %s", w.Code, w.Body.String())
	}
}

func TestSPAFallbackDoesNotHandleMissingAPI(t *testing.T) {
	h := Handler("/api")
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/demo/item", 200},
		{"/capabilities", 200},
		{"/api/missing", 404},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.code {
			t.Fatalf("GET %s = %d, want %d", tc.path, w.Code, tc.code)
		}
	}
}
