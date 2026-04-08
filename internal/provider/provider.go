package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const defaultHostname = "app.terraform.io"

var _ provider.Provider = &tfregistryProvider{}

// ProviderConfig holds the resolved provider configuration used by resources.
type ProviderConfig struct {
	// BaseURL is the HCP Terraform / TFE API base URL (e.g. https://app.terraform.io).
	BaseURL *url.URL
	// Token is the API authentication token.
	Token string
	// Organization is the default organization name.
	Organization string
	// HTTPClient is the HTTP client to use for API requests.
	HTTPClient *http.Client
}

type tfregistryProvider struct{}

type tfregistryProviderModel struct {
	Hostname      types.String `tfsdk:"hostname"`
	Token         types.String `tfsdk:"token"`
	SSLSkipVerify types.Bool   `tfsdk:"ssl_skip_verify"`
	Organization  types.String `tfsdk:"organization"`
}

func New() provider.Provider {
	return &tfregistryProvider{}
}

func (p *tfregistryProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "tfregistry"
}

func (p *tfregistryProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The tfregistry provider manages resources on the public Terraform registry via HCP Terraform.",
		Attributes: map[string]schema.Attribute{
			"hostname": schema.StringAttribute{
				Description: "The HCP Terraform or Terraform Enterprise hostname to connect to. Defaults to app.terraform.io. " +
					"Can also be set via the TFE_HOSTNAME environment variable.",
				Optional: true,
			},
			"token": schema.StringAttribute{
				Description: "The token used to authenticate with HCP Terraform / Terraform Enterprise. " +
					"Can also be set via the TFE_TOKEN environment variable.",
				Optional:  true,
				Sensitive: true,
			},
			"ssl_skip_verify": schema.BoolAttribute{
				Description: "Whether or not to skip certificate verifications. " +
					"Can also be set via the TFE_SSL_SKIP_VERIFY environment variable.",
				Optional: true,
			},
			"organization": schema.StringAttribute{
				Description: "The organization to use by default for resources that require one. " +
					"Can also be set via the TFE_ORGANIZATION environment variable.",
				Optional: true,
			},
		},
	}
}

func (p *tfregistryProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data tfregistryProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve hostname
	hostname := valueOrEnv(data.Hostname, "TFE_HOSTNAME", defaultHostname)

	// Resolve token: provider config → TFE_TOKEN env var → Terraform CLI credentials file
	token := valueOrEnv(data.Token, "TFE_TOKEN", "")
	if token == "" {
		token = getTokenFromCredentialsFile(hostname)
	}
	if token == "" {
		resp.Diagnostics.AddError(
			"Missing API token",
			"A token must be provided via the provider configuration, the TFE_TOKEN environment variable, "+
				"or Terraform CLI credentials (terraform login).",
		)
		return
	}

	// Resolve SSL skip verify
	sslSkipVerify := false
	if !data.SSLSkipVerify.IsNull() {
		sslSkipVerify = data.SSLSkipVerify.ValueBool()
	} else if v := os.Getenv("TFE_SSL_SKIP_VERIFY"); v != "" {
		var err error
		sslSkipVerify, err = strconv.ParseBool(v)
		if err != nil {
			resp.Diagnostics.AddError(
				"Invalid TFE_SSL_SKIP_VERIFY value",
				fmt.Sprintf("TFE_SSL_SKIP_VERIFY has unrecognized value %q", v),
			)
			return
		}
	}

	// Resolve organization
	organization := valueOrEnv(data.Organization, "TFE_ORGANIZATION", "")

	// Build the base URL
	scheme := "https"
	baseURL := &url.URL{
		Scheme: scheme,
		Host:   hostname,
	}

	// Build HTTP client
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	transport.TLSClientConfig.InsecureSkipVerify = sslSkipVerify

	httpClient := &http.Client{
		Transport: transport,
	}

	config := &ProviderConfig{
		BaseURL:      baseURL,
		Token:        token,
		Organization: organization,
		HTTPClient:   httpClient,
	}

	resp.ResourceData = config
	resp.DataSourceData = config
}

func (p *tfregistryProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewPublicRegistryModuleResource,
	}
}

func (p *tfregistryProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// valueOrEnv returns the Terraform config value if set, otherwise the env var, otherwise the fallback.
func valueOrEnv(val types.String, envVar, fallback string) string {
	if !val.IsNull() && !val.IsUnknown() && val.ValueString() != "" {
		return val.ValueString()
	}
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return fallback
}

// getTokenFromCredentialsFile reads a token for the given hostname from the
// Terraform CLI credentials file (~/.terraform.d/credentials.tfrc.json or
// the platform-appropriate equivalent).
func getTokenFromCredentialsFile(hostname string) string {
	path := credentialsFilePath()
	if path == "" {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var creds struct {
		Credentials map[string]struct {
			Token string `json:"token"`
		} `json:"credentials"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return ""
	}

	if entry, ok := creds.Credentials[hostname]; ok {
		return entry.Token
	}
	return ""
}

// credentialsFilePath returns the path to the Terraform CLI credentials file.
func credentialsFilePath() string {
	// Check TF_CLI_CONFIG_FILE first — if set, credentials may be in a different
	// location, but the standard credentials.tfrc.json is separate from the main
	// config and always lives in the data dir.
	dir := os.Getenv("TF_DATA_DIR")
	if dir == "" {
		if runtime.GOOS == "windows" {
			appData := os.Getenv("APPDATA")
			if appData == "" {
				return ""
			}
			dir = filepath.Join(appData, "terraform.d")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return ""
			}
			dir = filepath.Join(home, ".terraform.d")
		}
	}
	return filepath.Join(dir, "credentials.tfrc.json")
}
