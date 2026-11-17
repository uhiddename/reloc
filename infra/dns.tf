data "cloudflare_zone" "main" {
  filter = {
    name = var.domain
  }
}

locals {
  # The server's own name (ssh, kubectl through ssh) and the app on it.
  # Not proxied: Cloudflare passes these as plain DNS, so names resolve to
  # the server itself. reloc gets proxied with Cloudflare Access later.
  hosts = {
    node  = "${var.name}.${var.domain}"
    reloc = "reloc.${var.domain}"
  }
}

resource "cloudflare_dns_record" "a" {
  for_each = local.hosts

  zone_id = data.cloudflare_zone.main.zone_id
  name    = each.value
  type    = "A"
  content = hcloud_server.node.ipv4_address
  ttl     = 300
  proxied = false
}

resource "cloudflare_dns_record" "aaaa" {
  for_each = local.hosts

  zone_id = data.cloudflare_zone.main.zone_id
  name    = each.value
  type    = "AAAA"
  content = hcloud_server.node.ipv6_address
  ttl     = 300
  proxied = false
}
