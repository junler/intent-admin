package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPrecompressedPrefersBrotliAndHonoursQZero(t *testing.T) {
	sub := fstest.MapFS{
		"app.js":     {Data: []byte("source")},
		"app.js.br":  {Data: []byte("brotli")},
		"app.js.gz":  {Data: []byte("gzipped")},
		"only.js":    {Data: []byte("source")},
		"only.js.gz": {Data: []byte("gzipped")},
		"plain.js":   {Data: []byte("source")},
	}
	for _, tc := range []struct {
		name, accept, wantBody, wantEncoding string
	}{
		{"app.js", "br, gzip", "brotli", "br"},
		{"app.js", "gzip", "gzipped", "gzip"},
		{"app.js", "br;q=0, gzip", "gzipped", "gzip"},
		{"app.js", "identity", "", ""},
		{"only.js", "br, gzip", "gzipped", "gzip"},
		{"plain.js", "br, gzip", "", ""},
	} {
		r := httptest.NewRequest("GET", "/"+tc.name, nil)
		r.Header.Set("Accept-Encoding", tc.accept)
		body, encoding, ok := precompressed(sub, tc.name, r)
		if ok != (tc.wantEncoding != "") {
			t.Fatalf("%s via %q: ok=%v", tc.name, tc.accept, ok)
		}
		if encoding != tc.wantEncoding || string(body) != tc.wantBody {
			t.Fatalf("%s via %q = %q/%s, want %q/%s", tc.name, tc.accept, body, encoding, tc.wantBody, tc.wantEncoding)
		}
	}
}

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
