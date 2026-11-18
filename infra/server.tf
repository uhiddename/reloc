resource "hcloud_ssh_key" "admin" {
  name       = "${var.name}-admin"
  public_key = file(pathexpand(var.ssh_public_key_file))
}

# Once attached, the firewall drops every inbound packet no rule allows;
# outbound stays open while there are no "out" rules. It runs in Hetzner's
# network, in front of the server, so nothing on the server can open a port
# by mistake.
resource "hcloud_firewall" "node" {
  name = var.name

  rule {
    description = "ssh"
    direction   = "in"
    protocol    = "tcp"
    port        = "22"
    source_ips  = ["0.0.0.0/0", "::/0"]
  }

  # Only https: certificates come through DNS-01, so port 80 is not needed.
  # The Kubernetes API (6443) stays closed; kubectl goes through ssh.
  #
  # And only from Cloudflare: reloc is proxied (dns.tf), so a request from
  # anywhere else went around Access, straight to the server's IP.
  rule {
    description = "https from Cloudflare"
    direction   = "in"
    protocol    = "tcp"
    port        = "443"
    source_ips = concat(
      data.cloudflare_ip_ranges.cloudflare.ipv4_cidrs,
      data.cloudflare_ip_ranges.cloudflare.ipv6_cidrs,
    )
  }

  rule {
    description = "ping"
    direction   = "in"
    protocol    = "icmp"
    source_ips  = ["0.0.0.0/0", "::/0"]
  }
}

# The addresses Cloudflare connects to servers from; they change rarely,
# and a plan shows when they did.
data "cloudflare_ip_ranges" "cloudflare" {}

resource "hcloud_server" "node" {
  name         = var.name
  server_type  = var.server_type
  location     = var.location
  image        = "debian-13"
  ssh_keys     = [hcloud_ssh_key.admin.id]
  firewall_ids = [hcloud_firewall.node.id]

  public_net {
    ipv4_enabled = true
    ipv6_enabled = true
  }

  # Neither destroy nor rebuild works until these are set to false and
  # applied: a typo in the plan can't wipe the server. The provider wants
  # both to be the same.
  delete_protection  = true
  rebuild_protection = true

  labels = {
    cluster = var.name
  }

  lifecycle {
    # ssh_keys only matter at creation; changing them would replace the
    # server. Keys on a running server are managed by Ansible.
    ignore_changes = [ssh_keys]
  }
}
