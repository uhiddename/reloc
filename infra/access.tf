# Cloudflare Access in front of reloc: the name is proxied (dns.tf), and
# every request passes Cloudflare, which lets through only the people in
# var.access_emails, after a one-time code sent to their mail, and the
# reloc CLI with its service token. Everyone else gets Cloudflare's login
# page; relocd never sees them.
#
# Access belongs to the account, not to the zone; the account is the one
# hiddename.dev is in.
locals {
  account_id = data.cloudflare_zone.main.account.id
}

# The Zero Trust organization, made by hand in the dashboard; its team
# domain (divine-band-6bfb.cloudflareaccess.com) has the login page and
# issues the JWTs.
data "cloudflare_zero_trust_organization" "main" {
  account_id = local.account_id
}

# Login by a code sent to the mail: no passwords, no third-party accounts.
resource "cloudflare_zero_trust_access_identity_provider" "otp" {
  account_id = local.account_id
  name       = "One-time code"
  type       = "onetimepin"
  config     = {}
}

resource "cloudflare_zero_trust_access_policy" "people" {
  account_id = local.account_id
  name       = "reloc people"
  decision   = "allow"
  include = [
    for email in var.access_emails : { email = { email = email } }
  ]
}

# The CLI can't go through a login page. Instead it sends the token's
# CF-Access-Client-Id and CF-Access-Client-Secret headers; non_identity
# means the policy checks the token, not a person. The secret ends up in
# the state, as anything Terraform creates does.
resource "cloudflare_zero_trust_access_service_token" "cli" {
  account_id = local.account_id
  name       = "reloc-cli"
  # A year, the default, written out: after that the CLI gets 401 and the
  # token has to be rotated (client_secret_version).
  duration = "8760h"
}

resource "cloudflare_zero_trust_access_policy" "cli" {
  account_id = local.account_id
  name       = "reloc CLI"
  decision   = "non_identity"
  include = [
    { service_token = { token_id = cloudflare_zero_trust_access_service_token.cli.id } }
  ]
}

resource "cloudflare_zero_trust_access_application" "reloc" {
  account_id = local.account_id
  name       = "reloc"
  type       = "self_hosted"

  # The whole host: the UI, the API and the probes.
  destinations = [
    { type = "public", uri = local.hosts.reloc }
  ]

  policies = [
    { id = cloudflare_zero_trust_access_policy.people.id, precedence = 1 },
    { id = cloudflare_zero_trust_access_policy.cli.id, precedence = 2 },
  ]

  # Only the code by mail, so skip the page that asks which login to use.
  allowed_idps              = [cloudflare_zero_trust_access_identity_provider.otp.id]
  auto_redirect_to_identity = true

  # How long a login lasts before the next code.
  session_duration = "24h"

  # The CLI gets a plain 401 for a wrong token, not a redirect to a login
  # page it can't show.
  service_auth_401_redirect = true

  # The Access cookie: not readable from JavaScript, not sent along with
  # requests that other sites start.
  http_only_cookie_attribute = true
  same_site_cookie_attribute = "lax"

  app_launcher_visible = false
}

# Cloudflare connects to k0 over https and checks its certificate (the one
# from Let's Encrypt), so nobody between Cloudflare and k0 can pretend to
# be k0. The default, "full", takes any certificate, even a self-signed one.
resource "cloudflare_zone_setting" "ssl" {
  zone_id    = data.cloudflare_zone.main.zone_id
  setting_id = "ssl"
  value      = "strict"
}

# Cloudflare listens on port 80 for proxied names; send those to https.
resource "cloudflare_zone_setting" "always_use_https" {
  zone_id    = data.cloudflare_zone.main.zone_id
  setting_id = "always_use_https"
  value      = "on"
}
