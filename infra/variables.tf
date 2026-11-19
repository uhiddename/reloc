variable "name" {
  description = "Name of the server and of the cluster on it; also its DNS name under the domain (k0.hiddename.dev)."
  type        = string
  default     = "k0"
}

variable "server_type" {
  description = "Hetzner server type: cx23 is 2 shared x86 vCPU, 4 GB RAM, 40 GB disk."
  type        = string
  default     = "cx23"
}

variable "location" {
  description = "Hetzner location: fsn1 and nbg1 (Germany) or hel1 (Finland)."
  type        = string
  default     = "hel1"
}

variable "ssh_public_key_file" {
  description = "Public key put into root's authorized_keys when the server is created."
  type        = string
  default     = "~/.ssh/keys/reloc-vps/id_ed25519.pub"
}

variable "domain" {
  description = "Cloudflare zone the DNS records go into."
  type        = string
  default     = "hiddename.dev"
}

# Mail addresses are personal and the repository may become public, so the
# list is in terraform.tfvars, which is not committed; see
# terraform.tfvars.example.
variable "access_emails" {
  description = "Who Cloudflare Access lets into reloc, after a one-time code sent to the address."
  type        = list(string)
}

# Personal, like the mail addresses: in terraform.tfvars.
variable "admin_ips" {
  description = "Where ssh to the server is allowed from, as CIDRs: the home IPv4 as /32, and its IPv6 prefix if home has one."
  type        = list(string)

  validation {
    condition     = length(var.admin_ips) > 0 && alltrue([for cidr in var.admin_ips : can(cidrhost(cidr, 0))])
    error_message = "admin_ips must be a non-empty list of CIDRs, like 203.0.113.7/32."
  }
}
