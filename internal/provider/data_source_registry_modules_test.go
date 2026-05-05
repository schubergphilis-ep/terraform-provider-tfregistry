package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBuildListModulesURL(t *testing.T) {
	base, _ := url.Parse("https://app.terraform.io")

	tests := []struct {
		name           string
		org            string
		registryName   string
		search         string
		pageNum        int
		wantPath       string
		wantQueryParts []string
		notWantQuery   []string
	}{
		{
			name:     "minimal",
			org:      "my-org",
			pageNum:  1,
			wantPath: "/api/v2/organizations/my-org/registry-modules",
			wantQueryParts: []string{
				"page%5Bnumber%5D=1",
				"page%5Bsize%5D=100",
			},
			notWantQuery: []string{"filter%5Bregistry_name%5D", "q="},
		},
		{
			name:         "with filters",
			org:          "my-org",
			registryName: "private",
			search:       "vpc",
			pageNum:      3,
			wantPath:     "/api/v2/organizations/my-org/registry-modules",
			wantQueryParts: []string{
				"page%5Bnumber%5D=3",
				"page%5Bsize%5D=100",
				"filter%5Bregistry_name%5D=private",
				"q=vpc",
			},
		},
		{
			name:     "org with special chars escaped",
			org:      "my org",
			pageNum:  1,
			wantPath: "/api/v2/organizations/my%20org/registry-modules",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildListModulesURL(base, tt.org, tt.registryName, tt.search, tt.pageNum)
			if !strings.Contains(got, tt.wantPath) {
				t.Errorf("URL %q does not contain expected path %q", got, tt.wantPath)
			}
			for _, part := range tt.wantQueryParts {
				if !strings.Contains(got, part) {
					t.Errorf("URL %q missing expected query part %q", got, part)
				}
			}
			for _, part := range tt.notWantQuery {
				if strings.Contains(got, part) {
					t.Errorf("URL %q should not contain query part %q", got, part)
				}
			}
		})
	}
}

func TestBuildModulesDataSourceID(t *testing.T) {
	tests := []struct {
		name         string
		organization string
		registryName string
		search       string
		want         string
	}{
		{"org only", "my-org", "", "", "my-org"},
		{"org and registry", "my-org", "private", "", "my-org/private"},
		{"org and search", "my-org", "", "vpc", "my-org?q=vpc"},
		{"all three", "my-org", "private", "vpc", "my-org/private?q=vpc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildModulesDataSourceID(tt.organization, tt.registryName, tt.search)
			if got != tt.want {
				t.Errorf("buildModulesDataSourceID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestListRegistryModules_Single(t *testing.T) {
	resp := listModulesResponse{
		Data: []listModulesEntry{
			fakeListEntry("mod-1", "vpc", "aws"),
			fakeListEntry("mod-2", "network", "google"),
		},
	}
	resp.Meta.Pagination.CurrentPage = 1
	resp.Meta.Pagination.TotalPages = 1
	// NextPage is nil → no more pages

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %q", got)
		}
		if !strings.Contains(r.URL.Path, "/api/v2/organizations/my-org/registry-modules") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	base, _ := url.Parse(server.URL)
	entries, err := listRegistryModules(context.Background(), server.Client(), "test-token", base, "my-org", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Attributes.Name != "vpc" {
		t.Errorf("expected name 'vpc', got %q", entries[0].Attributes.Name)
	}
}

func TestListRegistryModules_Pagination(t *testing.T) {
	page1 := listModulesResponse{
		Data: []listModulesEntry{fakeListEntry("mod-1", "vpc", "aws")},
	}
	nextPage := 2
	page1.Meta.Pagination.CurrentPage = 1
	page1.Meta.Pagination.NextPage = &nextPage
	page1.Meta.Pagination.TotalPages = 2

	page2 := listModulesResponse{
		Data: []listModulesEntry{fakeListEntry("mod-2", "network", "google")},
	}
	page2.Meta.Pagination.CurrentPage = 2
	page2.Meta.Pagination.TotalPages = 2

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page[number]") {
		case "1":
			_ = json.NewEncoder(w).Encode(page1)
		case "2":
			_ = json.NewEncoder(w).Encode(page2)
		default:
			t.Errorf("unexpected page number: %s", r.URL.Query().Get("page[number]"))
		}
	}))
	defer server.Close()

	base, _ := url.Parse(server.URL)
	entries, err := listRegistryModules(context.Background(), server.Client(), "test-token", base, "my-org", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ID != "mod-1" || entries[1].ID != "mod-2" {
		t.Errorf("entries out of order: %s, %s", entries[0].ID, entries[1].ID)
	}
}

func TestListRegistryModules_Filters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("filter[registry_name]"); got != "private" {
			t.Errorf("expected filter[registry_name]=private, got %q", got)
		}
		if got := r.URL.Query().Get("q"); got != "vpc" {
			t.Errorf("expected q=vpc, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := listModulesResponse{Data: []listModulesEntry{}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	base, _ := url.Parse(server.URL)
	_, err := listRegistryModules(context.Background(), server.Client(), "test-token", base, "my-org", "private", "vpc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListRegistryModules_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := listModulesResponse{Data: []listModulesEntry{}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	base, _ := url.Parse(server.URL)
	entries, err := listRegistryModules(context.Background(), server.Client(), "test-token", base, "my-org", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestListRegistryModules_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"errors":[{"detail":"boom"}]}`)
	}))
	defer server.Close()

	base, _ := url.Parse(server.URL)
	_, err := listRegistryModules(context.Background(), server.Client(), "test-token", base, "my-org", "", "")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestListRegistryModules_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `not json at all`)
	}))
	defer server.Close()

	base, _ := url.Parse(server.URL)
	_, err := listRegistryModules(context.Background(), server.Client(), "test-token", base, "my-org", "", "")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func fakeListEntry(id, name, provider string) listModulesEntry {
	e := listModulesEntry{ID: id, Type: "registry-modules"}
	e.Attributes.Name = name
	e.Attributes.Namespace = "my-org"
	e.Attributes.RegistryName = "private"
	e.Attributes.Provider = provider
	e.Attributes.Status = "setup_complete"
	e.Attributes.CreatedAt = "2024-01-01T00:00:00Z"
	e.Attributes.UpdatedAt = "2024-01-02T00:00:00Z"
	return e
}
