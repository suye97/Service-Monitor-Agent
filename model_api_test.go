package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestFetchModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("unexpected model request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing model API authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-b"},{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	defer server.Close()
	app, err := NewApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	models, err := app.FetchModels("custom", server.URL+"/v1", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(models, []string{"model-a", "model-b"}) {
		t.Fatalf("unexpected models: %v", models)
	}
}

func TestModelAPIURLMigratesLegacyEndpoint(t *testing.T) {
	got, err := modelAPIURL("https://api.example.com/v1/chat/completions", "models")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.example.com/v1/models" {
		t.Fatalf("unexpected model URL %q", got)
	}
	if _, err = modelAPIURL("http://example.com", "models"); err == nil {
		t.Fatal("insecure remote model URL accepted")
	}
}
