# Publish a module to the public Terraform Registry using a GitHub App connection
resource "tfregistry_module" "example" {
  organization = "my-organization"

  vcs_repo {
    identifier                  = "my-github-org/terraform-aws-my-module"
    github_app_installation_id  = "ghain-xxxxxxxxxxxx"
  }
}

# Publish a module using an OAuth VCS connection
resource "tfregistry_module" "oauth_example" {
  organization = "my-organization"

  vcs_repo {
    identifier     = "my-github-org/terraform-aws-my-module"
    oauth_token_id = "ot-xxxxxxxxxxxx"
  }
}
