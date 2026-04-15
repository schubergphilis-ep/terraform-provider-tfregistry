package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// fetchRegistryAccessToken exchanges the HCP Terraform API token for a short-lived
// registry.terraform.io access token scoped to the given organization.
func fetchRegistryAccessToken(ctx context.Context, httpClient *http.Client, tfeToken string, baseURL *url.URL, org string) (string, error) {
	tokenURL := fmt.Sprintf("%s://%s/api/v2/organizations/%s/registry/access_token",
		baseURL.Scheme, baseURL.Host, url.PathEscape(org))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating registry token request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+tfeToken)
	httpReq.Header.Set("Content-Type", "application/vnd.api+json")
	httpReq.Header.Set("Accept", "application/json")

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("fetching registry access token: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return "", fmt.Errorf("registry access token endpoint returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("decoding registry access token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("registry access token response contained no token")
	}
	return tokenResp.AccessToken, nil
}
