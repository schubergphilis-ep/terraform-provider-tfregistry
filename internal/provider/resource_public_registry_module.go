package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &publicRegistryModuleResource{}
var _ resource.ResourceWithConfigure = &publicRegistryModuleResource{}
var _ resource.ResourceWithImportState = &publicRegistryModuleResource{}

func NewPublicRegistryModuleResource() resource.Resource {
	return &publicRegistryModuleResource{}
}

type publicRegistryModuleResource struct {
	config *ProviderConfig
}

// --- Terraform state / plan models ---

type publicRegistryModuleModel struct {
	ID             types.String                      `tfsdk:"id"`
	Organization   types.String                      `tfsdk:"organization"`
	Name           types.String                      `tfsdk:"name"`
	Namespace      types.String                      `tfsdk:"namespace"`
	ModuleProvider types.String                      `tfsdk:"module_provider"`
	VCSRepo        *publicRegistryModuleVCSRepoModel `tfsdk:"vcs_repo"`
}

type publicRegistryModuleVCSRepoModel struct {
	Identifier        types.String `tfsdk:"identifier"`
	GHAInstallationID types.String `tfsdk:"github_app_installation_id"`
	OAuthTokenID      types.String `tfsdk:"oauth_token_id"`
}

// --- API request / response types ---

type createModuleRequest struct {
	Data createModuleData `json:"data"`
}

type createModuleData struct {
	Attributes       createModuleAttrs `json:"attributes"`
	OrganizationName string            `json:"organization_name"`
}

type createModuleAttrs struct {
	VCSRepo createModuleVCSRepo `json:"vcs_repo"`
}

type createModuleVCSRepo struct {
	Identifier        string `json:"identifier"`
	GHAInstallationID string `json:"github_app_installation_id,omitempty"`
	OAuthTokenID      string `json:"oauth_token_id,omitempty"`
}

type registryModuleEntry struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	System    string `json:"system"`
	SourceURL string `json:"source-url"`
}

// --- Resource interface ---

func (r *publicRegistryModuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_module"
}

func (r *publicRegistryModuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Publishes a Terraform module to the public registry from a VCS repository via HCP Terraform. " +
			"The module name and provider are inferred from the repository name, which must follow " +
			"the terraform-<PROVIDER>-<NAME> naming convention.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The ID of the public registry module (format: namespace/name/provider).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization": schema.StringAttribute{
				Description: "Name of the organization. If omitted, organization must be defined in the provider config.",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the module. Computed from the VCS repository name (terraform-<PROVIDER>-<NAME> convention).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"namespace": schema.StringAttribute{
				Description: "The namespace of the module on the public registry.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"module_provider": schema.StringAttribute{
				Description: "The provider of the module. Computed from the VCS repository name (terraform-<PROVIDER>-<NAME> convention).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"vcs_repo": schema.SingleNestedBlock{
				Description: "Settings for the registry module's VCS repository.",
				Attributes: map[string]schema.Attribute{
					"identifier": schema.StringAttribute{
						Description: "A reference to your VCS repository in the format <organization>/<repository>.",
						Required:    true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.RequiresReplace(),
						},
					},
					"oauth_token_id": schema.StringAttribute{
						Description: "Token ID of the VCS Connection (OAuth Connection Token) to use.",
						Optional:    true,
						Validators: []validator.String{
							stringvalidator.ExactlyOneOf(
								path.MatchRelative().AtParent().AtName("github_app_installation_id"),
							),
						},
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.RequiresReplace(),
						},
					},
					"github_app_installation_id": schema.StringAttribute{
						Description: "The installation ID of the GitHub App.",
						Optional:    true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.RequiresReplace(),
						},
					},
				},
			},
		},
	}
}

func (r *publicRegistryModuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	config, ok := req.ProviderData.(*ProviderConfig)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource Configure type",
			fmt.Sprintf("Expected *ProviderConfig, got %T.", req.ProviderData),
		)
		return
	}
	r.config = config
}

func (r *publicRegistryModuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan publicRegistryModuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.VCSRepo == nil {
		resp.Diagnostics.AddError(
			"Missing required block",
			"The vcs_repo block is required.",
		)
		return
	}

	organization := r.resolveOrganization(plan.Organization)
	if organization == "" {
		resp.Diagnostics.AddError(
			"Missing organization",
			"No organization was specified on the resource or provider configuration.",
		)
		return
	}

	// Derive name and provider from the repo identifier
	repoName := repoNameFromIdentifier(plan.VCSRepo.Identifier.ValueString())
	moduleName, moduleProvider, err := parseModuleRepoName(repoName)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid repository name",
			fmt.Sprintf("Repository name %q does not follow the terraform-<PROVIDER>-<NAME> naming convention: %s", repoName, err),
		)
		return
	}

	// Build create payload
	createReq := createModuleRequest{
		Data: createModuleData{
			OrganizationName: organization,
			Attributes: createModuleAttrs{
				VCSRepo: createModuleVCSRepo{
					Identifier: plan.VCSRepo.Identifier.ValueString(),
				},
			},
		},
	}
	if !plan.VCSRepo.GHAInstallationID.IsNull() && plan.VCSRepo.GHAInstallationID.ValueString() != "" {
		createReq.Data.Attributes.VCSRepo.GHAInstallationID = plan.VCSRepo.GHAInstallationID.ValueString()
	}
	if !plan.VCSRepo.OAuthTokenID.IsNull() && plan.VCSRepo.OAuthTokenID.ValueString() != "" {
		createReq.Data.Attributes.VCSRepo.OAuthTokenID = plan.VCSRepo.OAuthTokenID.ValueString()
	}

	tflog.Debug(ctx, fmt.Sprintf("Creating public registry module from repository %s", plan.VCSRepo.Identifier.ValueString()))

	// POST /api/v2/organizations/{org}/registry/modules
	createURL := fmt.Sprintf("%s://%s/api/v2/organizations/%s/registry/modules",
		r.config.BaseURL.Scheme, r.config.BaseURL.Host, url.PathEscape(organization))

	body, err := json.Marshal(createReq)
	if err != nil {
		resp.Diagnostics.AddError("Unable to marshal create request", err.Error())
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", createURL, bytes.NewReader(body))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create HTTP request", err.Error())
		return
	}
	r.setAuthHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := r.config.HTTPClient.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create public registry module", err.Error())
		return
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"Unable to create public registry module",
			fmt.Sprintf("API returned status %d: %s", httpResp.StatusCode, string(respBody)),
		)
		return
	}

	// Try to extract namespace from response, fall back to organization name
	namespace := extractNamespaceFromResponse(respBody, organization)

	// Wait for the module to appear on the public registry
	tflog.Debug(ctx, "Waiting for module to appear on the public registry")
	deadline := time.Now().Add(5 * time.Minute)
	var found bool
	for time.Now().Before(deadline) {
		entry, err := readPublicRegistryModule(ctx, r.config.HTTPClient, namespace, moduleName, moduleProvider)
		if err == nil && entry != nil {
			namespace = entry.Namespace
			found = true
			break
		}
		time.Sleep(5 * time.Second)
	}
	if !found {
		tflog.Warn(ctx, "Module not yet visible on public registry after creation; proceeding with derived values")
	}

	syntheticID := fmt.Sprintf("%s/%s/%s", namespace, moduleName, moduleProvider)

	result := publicRegistryModuleModel{
		ID:             types.StringValue(syntheticID),
		Organization:   types.StringValue(organization),
		Name:           types.StringValue(moduleName),
		Namespace:      types.StringValue(namespace),
		ModuleProvider: types.StringValue(moduleProvider),
		VCSRepo:        plan.VCSRepo,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &result)...)
}

func (r *publicRegistryModuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state publicRegistryModuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading public registry module from registry.terraform.io")

	entry, err := readPublicRegistryModule(ctx, r.config.HTTPClient,
		state.Namespace.ValueString(), state.Name.ValueString(), state.ModuleProvider.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read public registry module", err.Error())
		return
	}
	if entry == nil {
		tflog.Debug(ctx, "Public registry module no longer exists")
		resp.State.RemoveResource(ctx)
		return
	}

	// Update computed fields, preserve VCS repo from state
	state.Name = types.StringValue(entry.Name)
	state.Namespace = types.StringValue(entry.Namespace)
	state.ModuleProvider = types.StringValue(entry.System)
	state.ID = types.StringValue(fmt.Sprintf("%s/%s/%s", entry.Namespace, entry.Name, entry.System))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *publicRegistryModuleResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "All attributes require replacement. This is a bug in the provider.")
}

func (r *publicRegistryModuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state publicRegistryModuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organization := state.Organization.ValueString()
	namespace := state.Namespace.ValueString()
	name := state.Name.ValueString()
	moduleProvider := state.ModuleProvider.ValueString()

	tflog.Debug(ctx, fmt.Sprintf("Deleting public registry module %s/%s/%s", namespace, name, moduleProvider))

	// DELETE /api/v2/organizations/{org}/registry/modules/{namespace}/{name}/{provider}
	deleteURL := fmt.Sprintf("%s://%s/api/v2/organizations/%s/registry/modules/%s/%s/%s",
		r.config.BaseURL.Scheme, r.config.BaseURL.Host,
		url.PathEscape(organization),
		url.PathEscape(namespace),
		url.PathEscape(name),
		url.PathEscape(moduleProvider))

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", deleteURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create delete request", err.Error())
		return
	}
	r.setAuthHeaders(httpReq)

	httpResp, err := r.config.HTTPClient.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete public registry module", err.Error())
		return
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != 200 && httpResp.StatusCode != 204 && httpResp.StatusCode != 404 {
		respBody, _ := io.ReadAll(httpResp.Body)
		resp.Diagnostics.AddError(
			"Unable to delete public registry module",
			fmt.Sprintf("API returned status %d: %s", httpResp.StatusCode, string(respBody)),
		)
	}
}

func (r *publicRegistryModuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Format: <ORGANIZATION>/<NAMESPACE>/<NAME>/<PROVIDER>
	s := strings.SplitN(req.ID, "/", 4)
	if len(s) != 4 {
		resp.Diagnostics.AddError(
			"Error importing public registry module",
			fmt.Sprintf("Invalid import format: %s (expected <ORGANIZATION>/<NAMESPACE>/<NAME>/<PROVIDER>)", req.ID),
		)
		return
	}

	organization := s[0]
	namespace := s[1]
	name := s[2]
	moduleProvider := s[3]

	entry, err := readPublicRegistryModule(ctx, r.config.HTTPClient, namespace, name, moduleProvider)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read public registry module during import", err.Error())
		return
	}
	if entry == nil {
		resp.Diagnostics.AddError(
			"Module not found",
			fmt.Sprintf("Module %s/%s/%s was not found on the public registry", namespace, name, moduleProvider),
		)
		return
	}

	syntheticID := fmt.Sprintf("%s/%s/%s", namespace, name, moduleProvider)

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), syntheticID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("namespace"), namespace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("module_provider"), moduleProvider)...)
}

// --- Helper methods ---

func (r *publicRegistryModuleResource) resolveOrganization(resourceOrg types.String) string {
	if !resourceOrg.IsNull() && !resourceOrg.IsUnknown() && resourceOrg.ValueString() != "" {
		return resourceOrg.ValueString()
	}
	return r.config.Organization
}

func (r *publicRegistryModuleResource) setAuthHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+r.config.Token)
	req.Header.Set("Accept", "application/json")
}

// --- Standalone helper functions ---

// repoNameFromIdentifier extracts the repository name from "org/repo-name".
func repoNameFromIdentifier(identifier string) string {
	parts := strings.SplitN(identifier, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return identifier
}

// parseModuleRepoName parses "terraform-<PROVIDER>-<NAME>" into (name, provider).
func parseModuleRepoName(repoName string) (name string, provider string, err error) {
	parts := strings.SplitN(repoName, "-", 3)
	if len(parts) != 3 || parts[0] != "terraform" {
		return "", "", fmt.Errorf("expected format terraform-<PROVIDER>-<NAME>, got %q", repoName)
	}
	return parts[2], parts[1], nil
}

// readPublicRegistryModule looks up a module on registry.terraform.io.
// Returns nil, nil if the module is not found.
func readPublicRegistryModule(ctx context.Context, httpClient *http.Client, namespace, name, provider string) (*registryModuleEntry, error) {
	pageNum := 1
	for {
		listURL := fmt.Sprintf(
			"https://registry.terraform.io/v3/modules?filter[namespace]=%s&page[number]=%d&page[size]=50",
			url.QueryEscape(namespace), pageNum,
		)

		httpReq, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		httpReq.Header.Set("Accept", "application/json")

		httpResp, err := httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("fetching modules from public registry: %w", err)
		}

		if httpResp.StatusCode != 200 {
			_ = httpResp.Body.Close()
			return nil, fmt.Errorf("public registry returned status %d", httpResp.StatusCode)
		}

		respBody, err := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading registry response: %w", err)
		}

		// The registry.terraform.io/v3/modules API returns a plain JSON array
		var entries []registryModuleEntry
		if err := json.Unmarshal(respBody, &entries); err != nil {
			// Fall back to a wrapped format {"data": [...]}
			var wrapped struct {
				Data []registryModuleEntry `json:"data"`
			}
			if err2 := json.Unmarshal(respBody, &wrapped); err2 != nil {
				return nil, fmt.Errorf("decoding registry response: %w", errors.Join(err, err2))
			}
			entries = wrapped.Data
		}

		for i := range entries {
			entry := &entries[i]
			if entry.Name == name && entry.System == provider {
				return entry, nil
			}
		}

		if len(entries) < 50 {
			break
		}
		pageNum++
	}

	return nil, nil
}

// extractNamespaceFromResponse tries to extract the namespace from the create API response.
func extractNamespaceFromResponse(respBody []byte, fallback string) string {
	var resp struct {
		Data struct {
			Attributes struct {
				Namespace string `json:"namespace"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err == nil && resp.Data.Attributes.Namespace != "" {
		return resp.Data.Attributes.Namespace
	}
	return fallback
}
