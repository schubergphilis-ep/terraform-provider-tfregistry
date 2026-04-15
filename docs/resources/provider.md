---
page_title: "tfregistry_provider Resource - tfregistry"
subcategory: ""
description: |-
  Publishes a Terraform provider to the public registry from a VCS repository via HCP Terraform. The provider name is inferred from the repository name, which must follow the terraform-provider-<NAME> naming convention.
---

# tfregistry_provider (Resource)

Publishes a Terraform provider to the public registry from a VCS repository via HCP Terraform. The provider name is inferred from the repository name, which must follow the `terraform-provider-<NAME>` naming convention.

## Example Usage

```terraform
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
```

## Schema

### Required

- `category` (String) The category for the provider. Changing this forces a new resource to be created. Must be one of: `asset`, `ci-cd`, `cloud-automation`, `communication-messaging`, `container-orchestration`, `database`, `data-management`, `infrastructure`, `logging-monitoring`, `networking`, `platform`, `security-authentication`, `utility`, `vcs`, `web`.

### Optional

- `organization` (String) Name of the organization. If omitted, organization must be defined in the provider config.
- `vcs_repo` (Block, Optional) Settings for the registry provider's VCS repository. (see [below for nested schema](#nestedblock--vcs_repo))

### Read-Only

- `id` (String) The ID of the public registry provider (format: namespace/name).
- `name` (String) The name of the provider. Computed from the VCS repository name (terraform-provider-<NAME> convention).
- `namespace` (String) The namespace of the provider on the public registry.

<a id="nestedblock--vcs_repo"></a>
### Nested Schema for `vcs_repo`

Required:

- `identifier` (String) A reference to your VCS repository in the format <organization>/<repository>.

Optional:

- `github_app_installation_id` (String) The installation ID of the GitHub App.
- `oauth_token_id` (String) Token ID of the VCS Connection (OAuth Connection Token) to use.

## Import

Import is supported using the following syntax:

The [`terraform import` command](https://developer.hashicorp.com/terraform/cli/commands/import) can be used, for example:

```shell
# Import format: <ORGANIZATION>/<NAMESPACE>/<NAME>
terraform import tfregistry_provider.example my-organization/my-namespace/myprovider
```

**Note:** After import, the `vcs_repo` field cannot be recovered from the public registry. A subsequent `terraform plan` will show it as requiring configuration. The `category` is recovered automatically from the registry during import.
