data "cloudflare_zone" "main" {
  filter = {
    name = var.domain
  }
}

locals {
  # The server's own name (ssh, kubectl through ssh) and the app on it.
  hosts = {
    node  = "${var.name}.${var.domain}"
    reloc = "reloc.${var.domain}"
  }

  # Proxied names resolve to Cloudflare, which forwards https to the server
  # and puts Access in front of it (access.tf). The node name is not
  # proxied: Cloudflare forwards only http(s), not ssh.
  proxied = toset(["reloc"])
}

resource "cloudflare_dns_record" "a" {
  for_each = local.hosts

  zone_id = data.cloudflare_zone.main.zone_id
  name    = each.value
  type    = "A"
  content = hcloud_server.node.ipv4_address
  proxied = contains(local.proxied, each.key)
  # Cloudflare sets the TTL of proxied records itself; 1 means automatic.
  ttl = contains(local.proxied, each.key) ? 1 : 300

  # Proxy only once Access is there, or reloc would be open through
  # Cloudflare for a moment.
  depends_on = [cloudflare_zero_trust_access_application.reloc]
}

resource "cloudflare_dns_record" "aaaa" {
  for_each = local.hosts

  zone_id = data.cloudflare_zone.main.zone_id
  name    = each.value
  type    = "AAAA"
  content = hcloud_server.node.ipv6_address
  proxied = contains(local.proxied, each.key)
  # Cloudflare sets the TTL of proxied records itself; 1 means automatic.
  ttl = contains(local.proxied, each.key) ? 1 : 300

  # Proxy only once Access is there, or reloc would be open through
  # Cloudflare for a moment.
  depends_on = [cloudflare_zero_trust_access_application.reloc]
}
