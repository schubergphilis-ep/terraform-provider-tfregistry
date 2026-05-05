package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ datasource.DataSource = &registryModulesDataSource{}
var _ datasource.DataSourceWithConfigure = &registryModulesDataSource{}

func NewRegistryModulesDataSource() datasource.DataSource {
	return &registryModulesDataSource{}
}

type registryModulesDataSource struct {
	config *ProviderConfig
}

// --- Terraform models ---

type registryModulesDataSourceModel struct {
	ID           types.String              `tfsdk:"id"`
	Organization types.String              `tfsdk:"organization"`
	RegistryName types.String              `tfsdk:"registry_name"`
	Search       types.String              `tfsdk:"search"`
	Modules      []registryModuleListEntry `tfsdk:"modules"`
}

type registryModuleListEntry struct {
	ID           types.String `tfsdk:"id"`
	Organization types.String `tfsdk:"organization"`
	Namespace    types.String `tfsdk:"namespace"`
	Name         types.String `tfsdk:"name"`
	RegistryName types.String `tfsdk:"registry_name"`
	Provider     types.String `tfsdk:"provider"`
	Status       types.String `tfsdk:"status"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

// --- API response types ---

type listModulesResponse struct {
	Data []listModulesEntry `json:"data"`
	Meta struct {
		Pagination struct {
			CurrentPage int  `json:"current-page"`
			NextPage    *int `json:"next-page"`
			TotalPages  int  `json:"total-pages"`
		} `json:"pagination"`
	} `json:"meta"`
}

type listModulesEntry struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Name         string `json:"name"`
		Namespace    string `json:"namespace"`
		RegistryName string `json:"registry-name"`
		Provider     string `json:"provider"`
		Status       string `json:"status"`
		CreatedAt    string `json:"created-at"`
		UpdatedAt    string `json:"updated-at"`
	} `json:"attributes"`
}

// --- Data source interface ---

func (d *registryModulesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_modules"
}

func (d *registryModulesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists registry modules in an HCP Terraform / Terraform Enterprise organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Synthetic identifier for the data source result.",
				Computed:    true,
			},
			"organization": schema.StringAttribute{
				Description: "Name of the organization. If omitted, organization must be defined in the provider config.",
				Optional:    true,
				Computed:    true,
			},
			"registry_name": schema.StringAttribute{
				Description: "Whether to list modules from the public or private registry. Must be either \"public\" or \"private\".",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("public", "private"),
				},
			},
			"search": schema.StringAttribute{
				Description: "A search query to filter modules by name.",
				Optional:    true,
			},
			"modules": schema.ListNestedAttribute{
				Description: "The list of registry modules.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The ID of the registry module.",
							Computed:    true,
						},
						"organization": schema.StringAttribute{
							Description: "The organization the module belongs to.",
							Computed:    true,
						},
						"namespace": schema.StringAttribute{
							Description: "The namespace of the module.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "The name of the module.",
							Computed:    true,
						},
						"registry_name": schema.StringAttribute{
							Description: "Whether the module is in the public or private registry.",
							Computed:    true,
						},
						"provider": schema.StringAttribute{
							Description: "The provider of the module.",
							Computed:    true,
						},
						"status": schema.StringAttribute{
							Description: "The status of the module.",
							Computed:    true,
						},
						"created_at": schema.StringAttribute{
							Description: "Time when the module was created.",
							Computed:    true,
						},
						"updated_at": schema.StringAttribute{
							Description: "Time when the module was last updated.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *registryModulesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	config, ok := req.ProviderData.(*ProviderConfig)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected data source Configure type",
			fmt.Sprintf("Expected *ProviderConfig, got %T.", req.ProviderData),
		)
		return
	}
	d.config = config
}

func (d *registryModulesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data registryModulesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organization := data.Organization.ValueString()
	if organization == "" {
		organization = d.config.Organization
	}
	if organization == "" {
		resp.Diagnostics.AddError(
			"Missing organization",
			"No organization was specified on the data source or provider configuration.",
		)
		return
	}

	registryName := data.RegistryName.ValueString()
	search := data.Search.ValueString()

	tflog.Debug(ctx, fmt.Sprintf("Listing registry modules in organization %s", organization))

	entries, err := listRegistryModules(ctx, d.config.HTTPClient, d.config.Token, d.config.BaseURL, organization, registryName, search)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list registry modules", err.Error())
		return
	}

	modules := make([]registryModuleListEntry, 0, len(entries))
	for _, e := range entries {
		modules = append(modules, registryModuleListEntry{
			ID:           types.StringValue(e.ID),
			Organization: types.StringValue(organization),
			Namespace:    types.StringValue(e.Attributes.Namespace),
			Name:         types.StringValue(e.Attributes.Name),
			RegistryName: types.StringValue(e.Attributes.RegistryName),
			Provider:     types.StringValue(e.Attributes.Provider),
			Status:       types.StringValue(e.Attributes.Status),
			CreatedAt:    types.StringValue(e.Attributes.CreatedAt),
			UpdatedAt:    types.StringValue(e.Attributes.UpdatedAt),
		})
	}

	data.ID = types.StringValue(buildModulesDataSourceID(organization, registryName, search))
	data.Organization = types.StringValue(organization)
	data.Modules = modules

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// --- Standalone helper functions ---

// listRegistryModules pages through the TFE list endpoint and returns all matching modules.
func listRegistryModules(ctx context.Context, httpClient *http.Client, token string, baseURL *url.URL, organization, registryName, search string) ([]listModulesEntry, error) {
	var all []listModulesEntry
	pageNum := 1
	for {
		listURL, err := buildListModulesURL(baseURL, organization, registryName, search, pageNum)
		if err != nil {
			return nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating list request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
		httpReq.Header.Set("Accept", "application/json")

		httpResp, err := httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("listing modules: %w", err)
		}

		respBody, readErr := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("reading response: %w", readErr)
		}

		if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
			return nil, fmt.Errorf("list modules endpoint returned status %d: %s", httpResp.StatusCode, string(respBody))
		}

		var page listModulesResponse
		if err := json.Unmarshal(respBody, &page); err != nil {
			return nil, fmt.Errorf("decoding list response: %w", err)
		}

		all = append(all, page.Data...)

		if page.Meta.Pagination.NextPage == nil {
			break
		}
		pageNum = *page.Meta.Pagination.NextPage
	}
	return all, nil
}

func buildListModulesURL(baseURL *url.URL, organization, registryName, search string, pageNum int) (string, error) {
	u := *baseURL
	u.Path = "/api/v2/organizations/" + organization + "/registry-modules"
	q := u.Query()
	q.Set("page[number]", fmt.Sprintf("%d", pageNum))
	q.Set("page[size]", "100")
	if registryName != "" {
		q.Set("filter[registry_name]", registryName)
	}
	if search != "" {
		q.Set("q", search)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func buildModulesDataSourceID(organization, registryName, search string) string {
	id := organization
	if registryName != "" {
		id += "/" + registryName
	}
	if search != "" {
		id += "?q=" + search
	}
	return id
}
