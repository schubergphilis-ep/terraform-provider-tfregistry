# Publish a provider to the public Terraform Registry using a GitHub App connection
resource "tfregistry_provider" "example" {
  organization = "my-organization"
  category     = "cloud-automation"

  vcs_repo {
    identifier                 = "my-github-org/terraform-provider-myprovider"
    github_app_installation_id = "ghain-xxxxxxxxxxxx"
  }
}

# Publish a provider using an OAuth VCS connection
resource "tfregistry_provider" "oauth_example" {
  organization = "my-organization"
  category     = "infrastructure"

  vcs_repo {
    identifier     = "my-github-org/terraform-provider-myprovider"
    oauth_token_id = "ot-xxxxxxxxxxxx"
  }
}
