// The `pear` Cloud Run service. It was created by hand with gcloud and imported here.
//
// Cloud Build (trigger 97bca61b) redeploys it on every push with
// `gcloud run services update --image=...`, so the image and the labels/annotations
// Cloud Build stamps on each revision are ignored below. Everything else is managed here.
//
// Secrets are referenced by name from Secret Manager and never appear in this repo or in
// state. The secret resources and their values are managed outside Terraform.

terraform {
  required_version = ">= 1.6"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }

  backend "gcs" {
    bucket = "virtual-charger-477003-g0-tfstate"
    prefix = "pear"
  }
}

variable "project" {
  type    = string
  default = "virtual-charger-477003-g0"
}

variable "region" {
  type    = string
  default = "us-west1"
}

// Meilisearch's VM address comes from ../meilisearch; apply that first.
data "terraform_remote_state" "meilisearch" {
  backend = "gcs"
  config = {
    bucket = "virtual-charger-477003-g0-tfstate"
    prefix = "meilisearch"
  }
}

locals {
  cloudsql_instance = "virtual-charger-477003-g0:us-west1:pear-db-usw1"

  secret_env = {
    HABITAT_DB                   = "habitat-pgurl"
    HABITAT_PDS_CRED_ENCRYPT_KEY = "habitat-pds-cred-encrypt-key"
    HABITAT_OAUTH_CLIENT_SECRET  = "habitat-oauth-client-secret"
    HABITAT_OAUTH_SERVER_SECRET  = "habitat-oauth-server-secret"
    HABITAT_GOOGLE_CLIENT_SECRET = "habitat-google-client-secret"
    HABITAT_SPACE_SIGNING_KEY    = "habitat-space-signing-key"
    HABITAT_NANGO_SECRET_KEY     = "habitat-nango-secret-key"
    OTEL_EXPORTER_OTLP_HEADERS   = "habitat-otlp-headers"
    HABITAT_MEILISEARCH_API_KEY  = "habitat-meilisearch-key"
  }

  plain_env = {
    HABITAT_DOMAIN              = "pear.habitat.network"
    HABITAT_HIVE_DOMAIN         = "id.habitat.network"
    HABITAT_BLOB_BUCKET         = "gs://pear-blobs"
    HABITAT_GOOGLE_CLIENT_ID    = "953995456319-667186r9ng84t5m4ibdne3380idub1ha.apps.googleusercontent.com"
    OTEL_EXPORTER_OTLP_ENDPOINT = "https://otlp-gateway-prod-us-west-0.grafana.net/otlp"
    HABITAT_MEILISEARCH_URL     = data.terraform_remote_state.meilisearch.outputs.meilisearch_url
  }
}

provider "google" {
  project = var.project
  region  = var.region
}

resource "google_cloud_run_v2_service" "pear" {
  name                 = "pear"
  location             = var.region
  ingress              = "INGRESS_TRAFFIC_ALL"
  invoker_iam_disabled = true // public without an allUsers IAM binding; PDS OAuth redirects need this.
  deletion_protection  = true

  template {
    service_account                  = "953995456319-compute@developer.gserviceaccount.com"
    timeout                          = "300s"
    max_instance_request_concurrency = 80
    session_affinity                 = false

    // Direct VPC egress so pear can reach the internal-only Meilisearch VM. Only private
    // ranges go through the VPC; public traffic (PDSes, OTLP, Google) egresses directly.
    vpc_access {
      network_interfaces {
        network    = "default"
        subnetwork = "default"
      }
      egress = "PRIVATE_RANGES_ONLY"
    }

    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [local.cloudsql_instance]
      }
    }

    containers {
      image = "us-west1-docker.pkg.dev/virtual-charger-477003-g0/cloud-run-source-deploy/habitat/habitat:57dee7c3de2fda41676ff9c8606c045b0f39caeb"

      ports {
        name           = "http1"
        container_port = 8000
      }

      resources {
        limits = {
          cpu    = "1000m"
          memory = "512Mi"
        }
        cpu_idle          = true // request-based CPU throttling
        startup_cpu_boost = true
      }

      startup_probe {
        failure_threshold = 1
        period_seconds    = 240
        timeout_seconds   = 240
        tcp_socket {
          port = 8000
        }
      }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }

      dynamic "env" {
        for_each = local.plain_env
        content {
          name  = env.key
          value = env.value
        }
      }

      dynamic "env" {
        for_each = local.secret_env
        content {
          name = env.key
          value_source {
            secret_key_ref {
              secret  = env.value
              version = "latest"
            }
          }
        }
      }
    }
  }

  traffic {
    type    = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
    percent = 100
  }

  lifecycle {
    ignore_changes = [
      template[0].containers[0].image,
      template[0].labels,
      template[0].annotations,
      labels,
      annotations,
      client,
      client_version,
    ]
  }
}
