package aiprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeDistinguishesAuthenticationAndToolSupport(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		tool   bool
		want   string
	}{{"invalid credential", 401, false, "invalid_key"}, {"no function support", 200, false, "unsupported_tool_calling"}, {"function support", 200, true, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private-test-key" {
					t.Error("missing authorization")
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.status != 200 {
					w.WriteHeader(tc.status)
					fmt.Fprint(w, `{"error":{"message":"bad key"}}`)
					return
				}
				if r.URL.Path == "/models" {
					fmt.Fprint(w, `{"data":[{"id":"test-model"}]}`)
					return
				}
				if r.URL.Path != "/chat/completions" {
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
				if tc.tool {
					fmt.Fprint(w, `{"id":"chat-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"ok\"}"}}]},"finish_reason":"tool_calls"}]}`)
				} else {
					fmt.Fprint(w, `{"id":"chat-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
				}
			}))
			defer server.Close()
			got := Probe(context.Background(), Config{Vendor: "custom", APIKey: "private-test-key", BaseURL: server.URL, Model: "test-model", TimeoutSec: 2})
			if got.ErrorKind != tc.want || got.OK != tc.tool {
				t.Fatalf("probe=%+v", got)
			}
			if strings.Contains(got.Error, "private-test-key") {
				t.Fatal("credential leaked")
			}
		})
	}
}
func TestProviderDefaultsAndValidation(t *testing.T) {
	p, ok := FindPreset("ark")
	if !ok || p.Adapter != "ark" {
		t.Fatal("Ark must use native adapter")
	}
	for _, endpoint := range []string{"file:///etc/passwd", "https://user:secret@example.com/v1", "https://example.com/v1?key=secret"} {
		if _, err := Normalize(Config{Vendor: "custom", BaseURL: endpoint, APIKey: "key", Model: "model"}); err == nil {
			t.Fatalf("accepted unsafe endpoint %s", endpoint)
		}
	}
}
