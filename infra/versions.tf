# Provider versions: ~> allows minor updates and no major ones (a major
# version of a provider may rename resources). The exact versions in use
# are in .terraform.lock.hcl, which is committed like go.sum.
terraform {
  required_version = ">= 1.10"

  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.70"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.29"
    }
  }
}

# Both providers read their tokens from the environment (HCLOUD_TOKEN,
# CLOUDFLARE_API_TOKEN, see .envrc.example), so no token is ever in a .tf
# file or in the state.
provider "hcloud" {}

provider "cloudflare" {}
