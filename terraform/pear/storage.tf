// The blob bucket and Cloud SQL instance pear runs against. Both were created by hand with
// gcloud and imported here (see the import blocks).
//
// Deliberately not managed: the tfstate bucket (it holds this configuration's own state),
// and the buckets Google creates and owns for Cloud Functions and Cloud Build
// (gcf-v2-*, *_cloudbuild). Database users and their passwords stay outside Terraform so
// no secret lands in state.

resource "google_storage_bucket" "pear_blobs" {
  name                        = "pear-blobs"
  location                    = "US"
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  soft_delete_policy {
    retention_duration_seconds = 604800
  }

  // The API reports an empty encryption block (no CMEK) that the provider would drop.
  lifecycle {
    ignore_changes = [encryption]
  }
}

import {
  to = google_storage_bucket.pear_blobs
  id = "${var.project}/pear-blobs"
}

resource "google_sql_database_instance" "pear" {
  name                = "pear-db-usw1"
  region              = var.region
  database_version    = "POSTGRES_18"
  deletion_protection = true

  settings {
    tier                  = "db-g1-small"
    edition               = "ENTERPRISE"
    availability_type     = "ZONAL"
    disk_type             = "PD_SSD"
    disk_size             = 10
    disk_autoresize       = true
    disk_autoresize_limit = 0

    deletion_protection_enabled = true
    enable_dataplex_integration = true

    database_flags {
      name  = "cloudsql.iam_authentication"
      value = "on"
    }

    backup_configuration {
      enabled                        = true
      start_time                     = "00:00"
      point_in_time_recovery_enabled = false
      transaction_log_retention_days = 7

      backup_retention_settings {
        retained_backups = 7
        retention_unit   = "COUNT"
      }
    }

    ip_configuration {
      ipv4_enabled = true
      ssl_mode     = "ALLOW_UNENCRYPTED_AND_ENCRYPTED"
    }

    location_preference {
      zone = "us-west1-a"
    }
  }

  // No maintenance window is set; the API reports day 0, which the provider can't send back.
  lifecycle {
    ignore_changes = [settings[0].maintenance_window]
  }
}

import {
  to = google_sql_database_instance.pear
  id = "${var.project}/pear-db-usw1"
}

resource "google_sql_database" "pear" {
  name     = "pear"
  instance = google_sql_database_instance.pear.name
}

import {
  to = google_sql_database.pear
  id = "projects/${var.project}/instances/pear-db-usw1/databases/pear"
}
