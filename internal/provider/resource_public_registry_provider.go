package provider

import (
	"bytes"
	"context"
	"encoding/json"
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

var _ resource.Resource = &publicRegistryProviderResource{}
var _ resource.ResourceWithConfigure = &publicRegistryProviderResource{}
var _ resource.ResourceWithImportState = &publicRegistryProviderResource{}

func NewPublicRegistryProviderResource() resource.Resource {
	return &publicRegistryProviderResource{}
}

type publicRegistryProviderResource struct {
	config *ProviderConfig
}

// --- Terraform state / plan models ---

type publicRegistryProviderModel struct {
	ID           types.String                        `tfsdk:"id"`
	Organization types.String                        `tfsdk:"organization"`
	Name         types.String                        `tfsdk:"name"`
	Namespace    types.String                        `tfsdk:"namespace"`
	Category     types.String                        `tfsdk:"category"`
	VCSRepo      *publicRegistryProviderVCSRepoModel `tfsdk:"vcs_repo"`
}

type publicRegistryProviderVCSRepoModel struct {
	Identifier        types.String `tfsdk:"identifier"`
	GHAInstallationID types.String `tfsdk:"github_app_installation_id"`
	OAuthTokenID      types.String `tfsdk:"oauth_token_id"`
}

// --- API request / response types ---

type createProviderRequest struct {
	Data createProviderData `json:"data"`
}

type createProviderData struct {
	Type       string              `json:"type"`
	Attributes createProviderAttrs `json:"attributes"`
}

type createProviderAttrs struct {
	Name         string                `json:"name"`
	Namespace    string                `json:"namespace"`
	RegistryName string                `json:"registry-name"`
	Category     string                `json:"category"`
	VCSRepo      createProviderVCSRepo `json:"vcs_repo"`
}

type createProviderVCSRepo struct {
	Identifier        string `json:"identifier"`
	GHAInstallationID string `json:"github_app_installation_id,omitempty"`
	OAuthTokenID      string `json:"oauth_token_id,omitempty"`
}

type registryProviderEntry struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Category  struct {
		Slug string `json:"slug"`
	} `json:"category"`
}

// --- Resource interface ---

func (r *publicRegistryProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider"
}

func (r *publicRegistryProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Publishes a Terraform provider to the public registry from a VCS repository via HCP Terraform. " +
			"The provider name is inferred from the repository name, which must follow " +
			"the terraform-provider-<NAME> naming convention.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The ID of the public registry provider (format: namespace/name).",
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
				Description: "The name of the provider. Computed from the VCS repository name (terraform-provider-<NAME> convention).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"namespace": schema.StringAttribute{
				Description: "The namespace of the provider on the public registry.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"category": schema.StringAttribute{
				Description: "The category for the provider. Changing this forces a new resource to be created.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf(
						"asset",
						"ci-cd",
						"cloud-automation",
						"communication-messaging",
						"container-orchestration",
						"database",
						"data-management",
						"infrastructure",
						"logging-monitoring",
						"networking",
						"platform",
						"security-authentication",
						"utility",
						"vcs",
						"web",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"vcs_repo": schema.SingleNestedBlock{
				Description: "Settings for the registry provider's VCS repository.",
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

func (r *publicRegistryProviderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *publicRegistryProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan publicRegistryProviderModel
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

	// Derive provider name from the repo identifier
	repoName := repoNameFromIdentifier(plan.VCSRepo.Identifier.ValueString())
	providerName, err := parseProviderRepoName(repoName)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid repository name",
			fmt.Sprintf("Repository name %q does not follow the terraform-provider-<NAME> naming convention: %s", repoName, err),
		)
		return
	}

	// Build create payload
	createReq := createProviderRequest{
		Data: createProviderData{
			Type: "registry-providers",
			Attributes: createProviderAttrs{
				Name:         providerName,
				Namespace:    organization,
				RegistryName: "public",
				Category:     plan.Category.ValueString(),
				VCSRepo: createProviderVCSRepo{
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

	tflog.Debug(ctx, fmt.Sprintf("Creating public registry provider from repository %s", plan.VCSRepo.Identifier.ValueString()))

	// POST /api/v2/organizations/{org}/registry/providers
	createURL := fmt.Sprintf("%s://%s/api/v2/organizations/%s/registry/providers",
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
	httpReq.Header.Set("Content-Type", "application/vnd.api+json")

	httpResp, err := r.config.HTTPClient.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create public registry provider", err.Error())
		return
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		resp.Diagnostics.AddError(
			"Unable to create public registry provider",
			fmt.Sprintf("API returned status %d: %s", httpResp.StatusCode, string(respBody)),
		)
		return
	}

	// Try to extract namespace from response, fall back to organization name
	namespace := extractNamespaceFromResponse(respBody, organization)

	// Wait for the provider to appear on the public registry
	tflog.Debug(ctx, "Waiting for provider to appear on the public registry")
	deadline := time.Now().Add(5 * time.Minute)
	var found bool
	for time.Now().Before(deadline) {
		entry, err := readPublicRegistryProvider(ctx, r.config.HTTPClient, namespace, providerName)
		if err == nil && entry != nil {
			namespace = entry.Namespace
			found = true
			break
		}
		time.Sleep(5 * time.Second)
	}
	if !found {
		tflog.Warn(ctx, "Provider not yet visible on public registry after creation; proceeding with derived values")
	}

	syntheticID := fmt.Sprintf("%s/%s", namespace, providerName)

	result := publicRegistryProviderModel{
		ID:           types.StringValue(syntheticID),
		Organization: types.StringValue(organization),
		Name:         types.StringValue(providerName),
		Namespace:    types.StringValue(namespace),
		Category:     plan.Category,
		VCSRepo:      plan.VCSRepo,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &result)...)
}

func (r *publicRegistryProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state publicRegistryProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Reading public registry provider from registry.terraform.io")

	entry, err := readPublicRegistryProvider(ctx, r.config.HTTPClient,
		state.Namespace.ValueString(), state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read public registry provider", err.Error())
		return
	}
	if entry == nil {
		tflog.Debug(ctx, "Public registry provider no longer exists")
		resp.State.RemoveResource(ctx)
		return
	}

	// Update computed fields and category from registry; preserve VCS repo from state
	state.Name = types.StringValue(entry.Name)
	state.Namespace = types.StringValue(entry.Namespace)
	state.ID = types.StringValue(fmt.Sprintf("%s/%s", entry.Namespace, entry.Name))
	if entry.Category.Slug != "" {
		state.Category = types.StringValue(entry.Category.Slug)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *publicRegistryProviderResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "All attributes require replacement. This is a bug in the provider.")
}

func (r *publicRegistryProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state publicRegistryProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organization := state.Organization.ValueString()
	namespace := state.Namespace.ValueString()
	name := state.Name.ValueString()

	tflog.Debug(ctx, fmt.Sprintf("Deleting public registry provider %s/%s", namespace, name))

	// DELETE /api/v2/organizations/{org}/registry/providers/{namespace}/{name}
	deleteURL := fmt.Sprintf("%s://%s/api/v2/organizations/%s/registry/providers/%s/%s",
		r.config.BaseURL.Scheme, r.config.BaseURL.Host,
		url.PathEscape(organization),
		url.PathEscape(namespace),
		url.PathEscape(name))

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", deleteURL, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create delete request", err.Error())
		return
	}
	r.setAuthHeaders(httpReq)

	httpResp, err := r.config.HTTPClient.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete public registry provider", err.Error())
		return
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != 200 && httpResp.StatusCode != 204 && httpResp.StatusCode != 404 {
		respBody, _ := io.ReadAll(httpResp.Body)
		resp.Diagnostics.AddError(
			"Unable to delete public registry provider",
			fmt.Sprintf("API returned status %d: %s", httpResp.StatusCode, string(respBody)),
		)
	}
}

func (r *publicRegistryProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Format: <ORGANIZATION>/<NAMESPACE>/<NAME>
	s := strings.SplitN(req.ID, "/", 3)
	if len(s) != 3 {
		resp.Diagnostics.AddError(
			"Error importing public registry provider",
			fmt.Sprintf("Invalid import format: %s (expected <ORGANIZATION>/<NAMESPACE>/<NAME>)", req.ID),
		)
		return
	}

	organization := s[0]
	namespace := s[1]
	name := s[2]

	entry, err := readPublicRegistryProvider(ctx, r.config.HTTPClient, namespace, name)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read public registry provider during import", err.Error())
		return
	}
	if entry == nil {
		resp.Diagnostics.AddError(
			"Provider not found",
			fmt.Sprintf("Provider %s/%s was not found on the public registry", namespace, name),
		)
		return
	}

	syntheticID := fmt.Sprintf("%s/%s", namespace, name)

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), syntheticID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization"), organization)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("namespace"), entry.Namespace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), entry.Name)...)
	if entry.Category.Slug != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("category"), entry.Category.Slug)...)
	}
}

// --- Helper methods ---

func (r *publicRegistryProviderResource) resolveOrganization(resourceOrg types.String) string {
	if !resourceOrg.IsNull() && !resourceOrg.IsUnknown() && resourceOrg.ValueString() != "" {
		return resourceOrg.ValueString()
	}
	return r.config.Organization
}

func (r *publicRegistryProviderResource) setAuthHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+r.config.Token)
	req.Header.Set("Accept", "application/json")
}

// --- Standalone helper functions ---

// parseProviderRepoName extracts the provider name from a "terraform-provider-<NAME>" repo name.
func parseProviderRepoName(repoName string) (string, error) {
	const prefix = "terraform-provider-"
	if !strings.HasPrefix(repoName, prefix) {
		return "", fmt.Errorf("expected format terraform-provider-<NAME>, got %q", repoName)
	}
	name := strings.TrimPrefix(repoName, prefix)
	if name == "" {
		return "", fmt.Errorf("provider name must not be empty in %q", repoName)
	}
	return name, nil
}

// readPublicRegistryProvider looks up a provider on registry.terraform.io.
// Returns nil, nil if the provider is not found.
func readPublicRegistryProvider(ctx context.Context, httpClient *http.Client, namespace, name string) (*registryProviderEntry, error) {
	pageNum := 1
	for {
		listURL := fmt.Sprintf(
			"https://registry.terraform.io/v3/providers?filter[namespace]=%s&include=latest-version,publishing-errors&page[number]=%d&page[size]=50",
			url.QueryEscape(namespace), pageNum,
		)

		httpReq, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		httpReq.Header.Set("Accept", "application/json")

		httpResp, err := httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("fetching providers from public registry: %w", err)
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

		// The registry.terraform.io/v3/providers API returns a plain JSON array
		var entries []registryProviderEntry
		if err := json.Unmarshal(respBody, &entries); err != nil {
			return nil, fmt.Errorf("decoding registry response: %w", err)
		}

		for i := range entries {
			if entries[i].Name == name {
				return &entries[i], nil
			}
		}

		if len(entries) < 50 {
			break
		}
		pageNum++
	}

	return nil, nil
}
