provider "tfregistry" {
  # The hostname of HCP Terraform or Terraform Enterprise.
  # Defaults to "app.terraform.io". Can also be set via TFE_HOSTNAME.
  # hostname = "app.terraform.io"

  # API token for authentication.
  # Can also be set via TFE_TOKEN, or retrieved automatically from
  # Terraform CLI credentials (terraform login).
  # token = "my-api-token"

  # Default organization for resources that require one.
  # Can also be set via TFE_ORGANIZATION.
  organization = "my-organization"
}
