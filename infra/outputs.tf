output "ipv4" {
  value = hcloud_server.node.ipv4_address
}

output "ipv6" {
  value = hcloud_server.node.ipv6_address
}

output "hosts" {
  value = local.hosts
}
