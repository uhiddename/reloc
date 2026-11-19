output "ipv4" {
  value = hcloud_server.node.ipv4_address
}

output "ipv6" {
  value = hcloud_server.node.ipv6_address
}

output "hosts" {
  value = local.hosts
}

# The CLI's Access token. The secret is not printed by plan or apply, only
# by terraform output -raw cli_access_client_secret.
output "cli_access_client_id" {
  value = cloudflare_zero_trust_access_service_token.cli.client_id
}

output "cli_access_client_secret" {
  value     = cloudflare_zero_trust_access_service_token.cli.client_secret
  sensitive = true
}

# relocd's RELOCD_CF_ACCESS_ISSUER and RELOCD_CF_ACCESS_AUDIENCE: whom the
# Access JWT must come from and for which application. Neither is a secret:
# both are in every token, and the issuer is on the login page.
output "cf_access_issuer" {
  value = "https://${data.cloudflare_zero_trust_organization.main.auth_domain}"
}

output "cf_access_aud" {
  value = cloudflare_zero_trust_access_application.reloc.aud
}
