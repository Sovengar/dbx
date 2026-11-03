package nl2sql

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sovengar/agentbridge"
)

func TestModelsListsTheProviderCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m-2"},{"id":"m-1"}]}`))
	}))
	defer server.Close()

	models, err := Models(context.Background(), Config{
		Provider: "local",
		Providers: map[string]ProviderConfig{
			"local": {BaseURL: server.URL},
		},
	})
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	want := []string{"m-1", "m-2"}
	if len(models) != len(want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Errorf("models[%d] = %q, want %q", i, models[i], want[i])
		}
	}
}

func TestModelSourceRejectsAProviderWithoutEndpoint(t *testing.T) {
	_, err := ModelSource(Config{Provider: "anthropic"})
	if err == nil {
		t.Fatal("want an error for a provider with no /models endpoint")
	}
}

func TestModelSourceIsTheSharedSeam(t *testing.T) {
	var source agentbridge.ModelSource
	source, err := ModelSource(Config{
		Provider: "local",
		Providers: map[string]ProviderConfig{
			"local": {BaseURL: "https://example.invalid/v1"},
		},
	})
	if err != nil {
		t.Fatalf("ModelSource: %v", err)
	}
	if source == nil {
		t.Fatal("ModelSource returned nil")
	}
}
