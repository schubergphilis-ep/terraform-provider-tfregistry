# List all registry modules in an organization.
data "tfregistry_modules" "all" {
  organization = "my-organization"
}

# List only private registry modules in an organization.
data "tfregistry_modules" "private" {
  organization  = "my-organization"
  registry_name = "private"
}

# Search for modules by name.
data "tfregistry_modules" "vpc" {
  organization = "my-organization"
  search       = "vpc"
}
