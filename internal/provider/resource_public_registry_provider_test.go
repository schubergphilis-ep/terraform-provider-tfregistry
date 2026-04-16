package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseProviderRepoName(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"terraform-provider-aws", "aws", false},
		{"terraform-provider-google-beta", "google-beta", false},
		{"terraform-provider-my-provider", "my-provider", false},
		{"terraform-provider-", "", true},
		{"not-terraform-provider-aws", "", true},
		{"aws", "", true},
		{"terraform-aws-vpc", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseProviderRepoName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseProviderRepoName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseProviderRepoName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// makeProviderEntry builds a registryProviderEntry matching the actual registry.terraform.io response shape.
func makeProviderEntry(name, categorySlug string) registryProviderEntry {
	const namespace = "test-ns"
	e := registryProviderEntry{
		Address:   fmt.Sprintf("%s/%s", namespace, name),
		Name:      name,
		Namespace: namespace,
	}
	e.Category.Slug = categorySlug
	return e
}

func TestReadPublicRegistryProvider_Found(t *testing.T) {
	entries := []registryProviderEntry{
		makeProviderEntry("aws", "cloud-automation"),
		makeProviderEntry("google", "infrastructure"),
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

	entry, err := readPublicRegistryProvider(context.Background(), client, "", "test-ns", "aws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if entry.Name != "aws" || entry.Namespace != "test-ns" || entry.Category.Slug != "cloud-automation" {
		t.Errorf("unexpected entry: %+v", entry)
	}
}

func TestReadPublicRegistryProvider_NotFound(t *testing.T) {
	entries := []registryProviderEntry{
		makeProviderEntry("aws", "cloud-automation"),
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

	entry, err := readPublicRegistryProvider(context.Background(), client, "", "test-ns", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil entry, got %+v", entry)
	}
}

func TestReadPublicRegistryProvider_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `not json at all`)
	}))
	defer server.Close()

	client := server.Client()
	originalTransport := client.Transport
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return originalTransport.RoundTrip(req)
	})

	_, err := readPublicRegistryProvider(context.Background(), client, "", "test-ns", "aws")
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
}

func TestReadPublicRegistryProvider_ServerError(t *testing.T) {
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

	_, err := readPublicRegistryProvider(context.Background(), client, "", "test-ns", "aws")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestReadPublicRegistryProvider_Pagination(t *testing.T) {
	page1 := make([]registryProviderEntry, 50)
	for i := range page1 {
		page1[i] = makeProviderEntry(fmt.Sprintf("provider-%d", i), "utility")
	}
	page2 := []registryProviderEntry{
		makeProviderEntry("target", "networking"),
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

	entry, err := readPublicRegistryProvider(context.Background(), client, "", "test-ns", "target")
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
