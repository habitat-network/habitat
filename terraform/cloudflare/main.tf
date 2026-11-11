// Cloudflare resources for habitat.network, imported from the live account.
//
// Worker *code* is deployed by wrangler (see each app's wrangler.jsonc) and is not managed
// here; Terraform owns the custom domains, routes, DNS records and D1 databases around it.
// The proxied `100::` AAAA records are created by Workers custom domains and live with them.
//
// Authenticate with CLOUDFLARE_API_TOKEN and set TF_VAR_account_id; nothing secret is stored here.

terraform {
  required_version = ">= 1.6"

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
  }

  backend "gcs" {
    bucket = "virtual-charger-477003-g0-tfstate"
    prefix = "cloudflare"
  }
}

variable "account_id" {
  type = string
}

locals {
  zone_id = "1970110c60211b179bb53b7e49dab8d6"

  // hostname => worker script name
  custom_domains = {
    "id.habitat.network"    = { id = "2b0edd23c5285614b82afd72a35826c751dc1174", service = "polished-salad-1db9" }
    "home.habitat.network"  = { id = "86544829fb7a96a5ffe7d54ddb697017f4e27e90", service = "frontend" }
    "api.habitat.network"   = { id = "81b54991379ba4767e4c88db1d2adc2f0375896f", service = "api-docs" }
    "chalk.habitat.network" = { id = "8c33dd5165a88a977fbb4aba0c358bafbed80ee9", service = "chalk" }
    "drop.habitat.network"  = { id = "7a900e229a1956b7c27d47747fc35af5110b55ed", service = "drop" }
    "brain.habitat.network" = { id = "ca5f06a6bea33102e1ab94e0109ae34df630a761", service = "brain" }
  }

  d1_databases = {
    brain = "57434a39-0d31-48db-8f4a-2522d07b45b6"
    drop  = "6b294689-d211-48c5-ba18-7c96d912fccc"
    chalk = "d8d8fd58-088a-47c7-8c24-7ec9e6483204"
  }
}

provider "cloudflare" {}

import {
  for_each = local.custom_domains
  to       = cloudflare_workers_custom_domain.this[each.key]
  id       = "${var.account_id}/${each.value.id}"
}

resource "cloudflare_workers_custom_domain" "this" {
  for_each    = local.custom_domains
  account_id  = var.account_id
  zone_id     = local.zone_id
  hostname    = each.key
  service     = each.value.service
  environment = "production"
}

import {
  to = cloudflare_workers_route.id_well_known
  id = "${local.zone_id}/11ce72d67aeb49b8847afd53b8271c22"
}

resource "cloudflare_workers_route" "id_well_known" {
  zone_id = local.zone_id
  pattern = "https://*.id.habitat.network/.well-known/*"
  script  = "polished-salad-1db9"
}

import {
  for_each = local.d1_databases
  to       = cloudflare_d1_database.this[each.key]
  id       = "${var.account_id}/${each.value}"
}

resource "cloudflare_d1_database" "this" {
  for_each   = local.d1_databases
  account_id = var.account_id
  name       = each.key

  // Live usage stats; they change as the apps write data.
  lifecycle {
    ignore_changes = [file_size, num_tables, version]
  }
}
