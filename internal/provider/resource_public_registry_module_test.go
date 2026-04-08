package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRepoNameFromIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"myorg/terraform-aws-vpc", "terraform-aws-vpc"},
		{"terraform-aws-vpc", "terraform-aws-vpc"},
		{"org/sub/repo", "sub/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := repoNameFromIdentifier(tt.input)
			if got != tt.want {
				t.Errorf("repoNameFromIdentifier(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseModuleRepoName(t *testing.T) {
	tests := []struct {
		input        string
		wantName     string
		wantProvider string
		wantErr      bool
	}{
		{"terraform-aws-vpc", "vpc", "aws", false},
		{"terraform-google-network", "network", "google", false},
		{"terraform-azurerm-my-module", "my-module", "azurerm", false},
		{"not-terraform-aws-vpc", "", "", true},
		{"terraform-aws", "", "", true},
		{"just-a-name", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, provider, err := parseModuleRepoName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseModuleRepoName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if name != tt.wantName {
				t.Errorf("parseModuleRepoName(%q) name = %q, want %q", tt.input, name, tt.wantName)
			}
			if provider != tt.wantProvider {
				t.Errorf("parseModuleRepoName(%q) provider = %q, want %q", tt.input, provider, tt.wantProvider)
			}
		})
	}
}

func TestExtractNamespaceFromResponse(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		fallback string
		want     string
	}{
		{
			name:     "valid response",
			body:     `{"data":{"attributes":{"namespace":"my-namespace"}}}`,
			fallback: "fallback",
			want:     "my-namespace",
		},
		{
			name:     "empty namespace uses fallback",
			body:     `{"data":{"attributes":{"namespace":""}}}`,
			fallback: "fallback",
			want:     "fallback",
		},
		{
			name:     "invalid JSON uses fallback",
			body:     `not json`,
			fallback: "fallback",
			want:     "fallback",
		},
		{
			name:     "missing attributes uses fallback",
			body:     `{"data":{}}`,
			fallback: "fallback",
			want:     "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractNamespaceFromResponse([]byte(tt.body), tt.fallback)
			if got != tt.want {
				t.Errorf("extractNamespaceFromResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadPublicRegistryModule_Found(t *testing.T) {
	entries := []registryModuleEntry{
		{Address: "test/vpc/aws", Name: "vpc", Namespace: "test-ns", System: "aws"},
		{Address: "test/network/google", Name: "network", Namespace: "test-ns", System: "google"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(entries); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	// Override the registry URL by using a custom client that redirects
	client := server.Client()
	originalTransport := client.Transport
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return originalTransport.RoundTrip(req)
	})

	entry, err := readPublicRegistryModule(context.Background(), client, "test-ns", "vpc", "aws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if entry.Name != "vpc" || entry.System != "aws" || entry.Namespace != "test-ns" {
		t.Errorf("unexpected entry: %+v", entry)
	}
}

func TestReadPublicRegistryModule_NotFound(t *testing.T) {
	entries := []registryModuleEntry{
		{Address: "test/vpc/aws", Name: "vpc", Namespace: "test-ns", System: "aws"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(entries); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := server.Client()
	originalTransport := client.Transport
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return originalTransport.RoundTrip(req)
	})

	entry, err := readPublicRegistryModule(context.Background(), client, "test-ns", "nonexistent", "aws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil entry, got %+v", entry)
	}
}

func TestReadPublicRegistryModule_WrappedFormat(t *testing.T) {
	body := `{"data":[{"address":"test/vpc/aws","name":"vpc","namespace":"test-ns","system":"aws"}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()

	client := server.Client()
	originalTransport := client.Transport
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return originalTransport.RoundTrip(req)
	})

	entry, err := readPublicRegistryModule(context.Background(), client, "test-ns", "vpc", "aws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if entry.Name != "vpc" {
		t.Errorf("expected name 'vpc', got %q", entry.Name)
	}
}

func TestReadPublicRegistryModule_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := server.Client()
	originalTransport := client.Transport
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return originalTransport.RoundTrip(req)
	})

	_, err := readPublicRegistryModule(context.Background(), client, "test-ns", "vpc", "aws")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestReadPublicRegistryModule_Pagination(t *testing.T) {
	page1 := make([]registryModuleEntry, 50)
	for i := range page1 {
		page1[i] = registryModuleEntry{
			Name: fmt.Sprintf("mod-%d", i), Namespace: "test-ns", System: "aws",
		}
	}
	page2 := []registryModuleEntry{
		{Name: "target", Namespace: "test-ns", System: "aws"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageNum := r.URL.Query().Get("page[number]")
		w.Header().Set("Content-Type", "application/json")
		if pageNum == "2" {
			if err := json.NewEncoder(w).Encode(page2); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			if err := json.NewEncoder(w).Encode(page1); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}
	}))
	defer server.Close()

	client := server.Client()
	originalTransport := client.Transport
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return originalTransport.RoundTrip(req)
	})

	entry, err := readPublicRegistryModule(context.Background(), client, "test-ns", "target", "aws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry on page 2, got nil")
	}
	if entry.Name != "target" {
		t.Errorf("expected name 'target', got %q", entry.Name)
	}
}

// roundTripFunc is an adapter to use a function as http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
