import {
  to = cloudflare_dns_record.a_wildcard_local_3837c8
  id = "${local.zone_id}/3837c8a744e8570c6cf30c247b61ad56"
}

resource "cloudflare_dns_record" "a_wildcard_local_3837c8" {
  zone_id = local.zone_id
  name    = "*.local.habitat.network"
  type    = "A"
  content = "127.0.0.1"
  ttl     = 1
  proxied = false
}

import {
  to = cloudflare_dns_record.a_local_d6d7bb
  id = "${local.zone_id}/d6d7bbe078e07f07968e6a43c03d9564"
}

resource "cloudflare_dns_record" "a_local_d6d7bb" {
  zone_id = local.zone_id
  name    = "local.habitat.network"
  type    = "A"
  content = "127.0.0.1"
  ttl     = 1
  proxied = false
}

import {
  to = cloudflare_dns_record.cname_google6c1a69da68acf158_95ffd1
  id = "${local.zone_id}/95ffd1fb8e733440290e2be1dec0f5ee"
}

resource "cloudflare_dns_record" "cname_google6c1a69da68acf158_95ffd1" {
  zone_id = local.zone_id
  name    = "google6c1a69da68acf158.habitat.network"
  type    = "CNAME"
  content = "google.com"
  ttl     = 1
  proxied = false
}

import {
  to = cloudflare_dns_record.cname_5cf28d
  id = "${local.zone_id}/5cf28dba7118a40a2a4265cfbc191437"
}

resource "cloudflare_dns_record" "cname_5cf28d" {
  zone_id = local.zone_id
  name    = "habitat.network"
  type    = "CNAME"
  content = "habitat-network.github.io"
  ttl     = 1
  proxied = false
}

import {
  to = cloudflare_dns_record.cname_wildcard_id_4c167f
  id = "${local.zone_id}/4c167fd890e69cfe88af953b3b37d65c"
}

resource "cloudflare_dns_record" "cname_wildcard_id_4c167f" {
  zone_id = local.zone_id
  name    = "*.id.habitat.network"
  type    = "CNAME"
  content = "id.habitat.network"
  ttl     = 1
  proxied = true
}

import {
  to = cloudflare_dns_record.cname_pear_48ae55
  id = "${local.zone_id}/48ae553834bbaf4aaf7294c1a646cdff"
}

resource "cloudflare_dns_record" "cname_pear_48ae55" {
  zone_id = local.zone_id
  name    = "pear.habitat.network"
  type    = "CNAME"
  content = "ghs.googlehosted.com"
  ttl     = 1
  proxied = false
  comment = "cloud run"
}

import {
  to = cloudflare_dns_record.cname_sap_chalk_e47263
  id = "${local.zone_id}/e472635675b8af7c1632e4a570e17c5b"
}

resource "cloudflare_dns_record" "cname_sap_chalk_e47263" {
  zone_id = local.zone_id
  name    = "sap.chalk.habitat.network"
  type    = "CNAME"
  content = "ghs.googlehosted.com"
  ttl     = 1
  proxied = false
}

import {
  to = cloudflare_dns_record.cname_www_dc20b9
  id = "${local.zone_id}/dc20b91e4457ab3b152c344d27ca2a15"
}

resource "cloudflare_dns_record" "cname_www_dc20b9" {
  zone_id = local.zone_id
  name    = "www.habitat.network"
  type    = "CNAME"
  content = "habitat-network.github.io"
  ttl     = 1
  proxied = false
}

import {
  to = cloudflare_dns_record.mx_17bd69
  id = "${local.zone_id}/17bd693b0ab3f3a689feec0b028f0560"
}

resource "cloudflare_dns_record" "mx_17bd69" {
  zone_id  = local.zone_id
  name     = "habitat.network"
  type     = "MX"
  content  = "route3.mx.cloudflare.net"
  ttl      = 1
  proxied  = false
  priority = 47
}

import {
  to = cloudflare_dns_record.mx_8a663d
  id = "${local.zone_id}/8a663da6f4e28d6746f12b902a853422"
}

resource "cloudflare_dns_record" "mx_8a663d" {
  zone_id  = local.zone_id
  name     = "habitat.network"
  type     = "MX"
  content  = "route2.mx.cloudflare.net"
  ttl      = 1
  proxied  = false
  priority = 87
}

import {
  to = cloudflare_dns_record.mx_b671c1
  id = "${local.zone_id}/b671c10ab128d8a5f09b37e7fa35dad8"
}

resource "cloudflare_dns_record" "mx_b671c1" {
  zone_id  = local.zone_id
  name     = "habitat.network"
  type     = "MX"
  content  = "route1.mx.cloudflare.net"
  ttl      = 1
  proxied  = false
  priority = 7
}

import {
  to = cloudflare_dns_record.mx_send_331824
  id = "${local.zone_id}/3318241593c86b01c4acb59b9d954c22"
}

resource "cloudflare_dns_record" "mx_send_331824" {
  zone_id  = local.zone_id
  name     = "send.habitat.network"
  type     = "MX"
  content  = "feedback-smtp.us-east-1.amazonses.com"
  ttl      = 3600
  proxied  = false
  priority = 10
}

import {
  to = cloudflare_dns_record.txt_atproto_946746
  id = "${local.zone_id}/946746924e31a5da5b6cf64152df2f40"
}

resource "cloudflare_dns_record" "txt_atproto_946746" {
  zone_id = local.zone_id
  name    = "_atproto.habitat.network"
  type    = "TXT"
  content = "\"did=did:plc:ss2uhsajrstfhkq73fteu4zz\""
  ttl     = 1
  proxied = false
}

import {
  to = cloudflare_dns_record.txt_cf2024_1_domainkey_4572dc
  id = "${local.zone_id}/4572dc7dfda3f2b22d428e3c13269aa3"
}

resource "cloudflare_dns_record" "txt_cf2024_1_domainkey_4572dc" {
  zone_id = local.zone_id
  name    = "cf2024-1._domainkey.habitat.network"
  type    = "TXT"
  content = "\"v=DKIM1; h=sha256; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAiweykoi+o48IOGuP7GR3X0MOExCUDY/BCRHoWBnh3rChl7WhdyCxW3jgq1daEjPPqoi7sJvdg5hEQVsgVRQP4DcnQDVjGMbASQtrY4WmB1VebF+RPJB2ECPsEDTpeiI5ZyUAwJaVX7r6bznU67g7LvFq35yIo4sdlmtZGV+i0H4cpYH9+3JJ78k\" \"m4KXwaf9xUJCWF6nxeD+qG6Fyruw1Qlbds2r85U9dkNDVAS3gioCvELryh1TxKGiVTkg4wqHTyHfWsp7KD3WQHYJn0RyfJJu6YEmL77zonn7p2SRMvTMP3ZEXibnC9gz3nnhR6wcYL8Q7zXypKTMD58bTixDSJwIDAQAB\""
  ttl     = 1
  proxied = false
}

import {
  to = cloudflare_dns_record.txt_3b0856
  id = "${local.zone_id}/3b08569a620b2c1bfae05f766a2c0737"
}

resource "cloudflare_dns_record" "txt_3b0856" {
  zone_id = local.zone_id
  name    = "habitat.network"
  type    = "TXT"
  content = "\"google-site-verification=wzOgqNbpJPWkPVGGXt7ANZJo8QmJIjEAf52MZy11sqM\""
  ttl     = 3600
  proxied = false
}

import {
  to = cloudflare_dns_record.txt_db2cab
  id = "${local.zone_id}/db2cabded2168f70609c9f3ceb98e17d"
}

resource "cloudflare_dns_record" "txt_db2cab" {
  zone_id = local.zone_id
  name    = "habitat.network"
  type    = "TXT"
  content = "\"google-site-verification=d54jWxZuJkl17dC_vsTF3-IlpxRgsAJWkh4XQTDRUQE\""
  ttl     = 3600
  proxied = false
}

import {
  to = cloudflare_dns_record.txt_d79501
  id = "${local.zone_id}/d7950123110d7424dc00e29261f27e36"
}

resource "cloudflare_dns_record" "txt_d79501" {
  zone_id = local.zone_id
  name    = "habitat.network"
  type    = "TXT"
  content = "\"v=spf1 include:_spf.mx.cloudflare.net include:_spf.google.com ~all\""
  ttl     = 1
  proxied = false
}

import {
  to = cloudflare_dns_record.txt_resend_domainkey_db60b3
  id = "${local.zone_id}/db60b3076bea51bb3ccbde9bf4bd9165"
}

resource "cloudflare_dns_record" "txt_resend_domainkey_db60b3" {
  zone_id = local.zone_id
  name    = "resend._domainkey.habitat.network"
  type    = "TXT"
  content = "\"p=MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDGQ9p/Rg9QFMMNY+P6mteJsjDAmVlzAnc9drfC+5tQx2kEjXRUKS4dFB20yNasRepMg12CsjKVxfAlppG8f/E85cLCMaYMkXjKVJZh74njw5i+n0eV90uXM3sKqFwY5hWxH5cx1UlPRhxeprcTSdtgjstjLSsBgQKqxQ/80fNV8QIDAQAB\""
  ttl     = 3600
  proxied = false
}

import {
  to = cloudflare_dns_record.txt_send_69fa85
  id = "${local.zone_id}/69fa859c096f4760ef03329e50f428f2"
}

resource "cloudflare_dns_record" "txt_send_69fa85" {
  zone_id = local.zone_id
  name    = "send.habitat.network"
  type    = "TXT"
  content = "\"v=spf1 include:amazonses.com ~all\""
  ttl     = 3600
  proxied = false
}

