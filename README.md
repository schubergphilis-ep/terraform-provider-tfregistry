# terraform-provider-tfregistry

A Terraform provider for publishing modules and providers to the [public Terraform Registry](https://registry.terraform.io/) from VCS repositories via HCP Terraform.

## Why this provider?

The official [tfe provider](https://registry.terraform.io/providers/hashicorp/tfe/latest) supports managing private registry modules, but does **not** support publishing modules or providers to the **public** Terraform Registry. This provider fills that gap, allowing you to automate public module and provider publishing from your VCS repositories using Terraform.

## Usage

```hcl
provider "tfregistry" {
  organization = "my-organization"
}

resource "tfregistry_module" "example" {
  vcs_repo {
    identifier                 = "my-github-org/terraform-aws-my-module"
    github_app_installation_id = "ghain-xxxxxxxxxxxx"
  }
}

resource "tfregistry_provider" "example" {
  category = "cloud-automation"

  vcs_repo {
    identifier                 = "my-github-org/terraform-provider-myprovider"
    github_app_installation_id = "ghain-xxxxxxxxxxxx"
  }
}
```

Authentication is resolved from (in order):

1. The `token` provider attribute
2. The `TFE_TOKEN` environment variable
3. Terraform CLI credentials (`terraform login`)

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.25 (to build the provider)

## Building

```bash
make build       # Compile provider binary
make install     # Build and install to local Terraform plugin directory
make test        # Unit tests
make testacc     # Acceptance tests (requires TF_ACC=1)
make generate    # Generate documentation via tfplugindocs
make lint        # Run golangci-lint
```

## Documentation

Documentation is auto-generated using [terraform-plugin-docs](https://github.com/hashicorp/terraform-plugin-docs) and can be found in the [`docs/`](docs/) directory.
