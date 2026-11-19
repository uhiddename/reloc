# Provider versions: ~> allows minor updates and no major ones (a major
# version of a provider may rename resources). The exact versions in use
# are in .terraform.lock.hcl, which is committed like go.sum.
terraform {
  required_version = ">= 1.10"

  # The state is in GitLab (the project's Operate > Terraform states), not
  # on a laptop: it holds secrets, like the CLI's Access token; GitLab
  # locks it while an apply runs, so two can't overlap, and keeps the old
  # versions. The login is TF_HTTP_USERNAME and TF_HTTP_PASSWORD, see
  # .envrc.example. "k0" is the state's name in GitLab.
  backend "http" {
    address        = "https://gitlab.com/api/v4/projects/88742741/terraform/state/k0"
    lock_address   = "https://gitlab.com/api/v4/projects/88742741/terraform/state/k0/lock"
    unlock_address = "https://gitlab.com/api/v4/projects/88742741/terraform/state/k0/lock"
    lock_method    = "POST"
    unlock_method  = "DELETE"
    retry_wait_min = 5
  }

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
