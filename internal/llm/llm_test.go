package llm

import (
	"net/http"
	"testing"
	"time"
)

func TestPickHTTPClientHonorsCustomClient(t *testing.T) {
	custom := &http.Client{Timeout: 42 * time.Second}
	if got := pickHTTPClient(custom); got != custom {
		t.Fatalf("expected custom client to be returned")
	}
}

func TestPickHTTPClientUsesLongerTimeout(t *testing.T) {
	client := pickHTTPClient(nil)
	if client.Timeout != defaultLLMHTTPTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultLLMHTTPTimeout, client.Timeout)
	}
}
