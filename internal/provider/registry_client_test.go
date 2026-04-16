package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFetchRegistryAccessToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-tfe-token" {
			t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/vnd.api+json" {
			t.Errorf("unexpected Content-Type header: %s", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"access_token": "reg-token-abc"}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	token, err := fetchRegistryAccessToken(context.Background(), server.Client(), "test-tfe-token", baseURL, "my-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "reg-token-abc" {
		t.Errorf("expected token 'reg-token-abc', got %q", token)
	}
}

func TestFetchRegistryAccessToken_NonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"status":"401","title":"Unauthorized"}]}`))
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	_, err := fetchRegistryAccessToken(context.Background(), server.Client(), "bad-token", baseURL, "my-org")
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
}

func TestFetchRegistryAccessToken_UnsupportedMediaType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		_, _ = w.Write([]byte(`{"errors":[{"status":"415","title":"invalid content type"}]}`))
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	_, err := fetchRegistryAccessToken(context.Background(), server.Client(), "test-token", baseURL, "my-org")
	if err == nil {
		t.Fatal("expected error for 415 response, got nil")
	}
}

func TestFetchRegistryAccessToken_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	_, err := fetchRegistryAccessToken(context.Background(), server.Client(), "test-token", baseURL, "my-org")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestFetchRegistryAccessToken_EmptyToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"access_token": ""}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	_, err := fetchRegistryAccessToken(context.Background(), server.Client(), "test-token", baseURL, "my-org")
	if err == nil {
		t.Fatal("expected error for empty token in response, got nil")
	}
}
