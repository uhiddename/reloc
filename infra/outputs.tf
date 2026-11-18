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
