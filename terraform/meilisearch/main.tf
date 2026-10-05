// Meilisearch for space record search (internal/search), run as the official
// container on a Container-Optimized OS VM with a persistent disk. Cloud Run is
// unsuitable: Meilisearch keeps an LMDB index on local disk that must survive restarts.
//
// The VM has no public IP. pear (Cloud Run) reaches it over the default VPC via Direct
// VPC egress; that wiring lives on the pear service in ../pear, which reads
// `meilisearch_url` from this config's state.

terraform {
  required_version = ">= 1.6"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  // State holds the generated master key, so keep this bucket private.
  // Create it once: gcloud storage buckets create gs://virtual-charger-477003-g0-tfstate \
  //   --location=us-west1 --uniform-bucket-level-access --public-access-prevention
  backend "gcs" {
    bucket = "virtual-charger-477003-g0-tfstate"
    prefix = "meilisearch"
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

variable "zone" {
  type    = string
  default = "us-west1-b"
}

// Keep in step with `meilisearch` in .prototools. Meilisearch indexes are not compatible
// across minor versions without a dump/import, so bump deliberately.
variable "meilisearch_version" {
  type    = string
  default = "v1.54.2"
}

// Cloud Run Direct VPC egress sends traffic from addresses in the subnet it is attached to.
variable "client_cidr" {
  type    = string
  default = "10.138.0.0/20"
}

provider "google" {
  project = var.project
  region  = var.region
  zone    = var.zone
}

resource "random_password" "master_key" {
  length  = 48
  special = false
}

resource "google_secret_manager_secret" "master_key" {
  secret_id = "habitat-meilisearch-key"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "master_key" {
  secret      = google_secret_manager_secret.master_key.id
  secret_data = random_password.master_key.result
}

resource "google_compute_disk" "data" {
  name = "meilisearch-data"
  type = "pd-balanced"
  size = 20
  zone = var.zone

  // The index is rebuildable from the repos, but a re-crawl is slow; don't lose it to a stray apply.
  lifecycle {
    prevent_destroy = true
  }
}

resource "google_compute_firewall" "meilisearch" {
  name          = "allow-meilisearch-internal"
  network       = "default"
  source_ranges = [var.client_cidr]
  target_tags   = ["meilisearch"]

  allow {
    protocol = "tcp"
    ports    = ["7700"]
  }
}

// The VM has no external IP, so it needs Cloud NAT to pull the image from Docker Hub.
// NAT is outbound-only; the VM stays unreachable from the internet.
resource "google_compute_router" "meilisearch" {
  name    = "meilisearch-router"
  network = "default"
  region  = var.region
}

resource "google_compute_router_nat" "meilisearch" {
  name                               = "meilisearch-nat"
  router                             = google_compute_router.meilisearch.name
  region                             = var.region
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"
}

// COS only ships container logs to Cloud Logging when the VM has a service account that can write them.
resource "google_service_account" "meilisearch" {
  account_id   = "meilisearch-vm"
  display_name = "Meilisearch VM"
}

resource "google_project_iam_member" "meilisearch_log_writer" {
  project = var.project
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.meilisearch.email}"
}

resource "google_compute_instance" "meilisearch" {
  name         = "meilisearch"
  machine_type = "e2-small"
  zone         = var.zone
  tags         = ["meilisearch"]

  boot_disk {
    initialize_params {
      image = "cos-cloud/cos-stable"
      size  = 10
    }
  }

  // device_name makes the disk appear at /dev/disk/by-id/google-meilisearch-data.
  attached_disk {
    source      = google_compute_disk.data.id
    device_name = "meilisearch-data"
  }

  network_interface {
    network = "default"
    // No access_config block: internal IP only.
  }

  service_account {
    email  = google_service_account.meilisearch.email
    scopes = ["cloud-platform"] // access is limited by the account's IAM roles instead
  }

  metadata = {
    google-logging-enabled = "true"
    user-data              = <<-EOT
      #cloud-config
      write_files:
        - path: /etc/meilisearch.env
          permissions: "0600"
          owner: root
          content: |
            MEILI_ENV=production
            MEILI_MASTER_KEY=${random_password.master_key.result}
            MEILI_DB_PATH=/meili_data/data.ms
            MEILI_DUMP_DIR=/meili_data/dumps
            MEILI_SNAPSHOT_DIR=/meili_data/snapshots
        - path: /etc/systemd/system/meilisearch.service
          permissions: "0644"
          owner: root
          content: |
            [Unit]
            Description=Meilisearch
            After=gcr-online.target docker.socket
            Wants=gcr-online.target

            [Service]
            # Format the disk only on first boot, when it has no filesystem.
            ExecStartPre=/bin/sh -c 'blkid /dev/disk/by-id/google-meilisearch-data || mkfs.ext4 -m 0 /dev/disk/by-id/google-meilisearch-data'
            ExecStartPre=/bin/mkdir -p /mnt/disks/meili
            ExecStartPre=/bin/sh -c 'mountpoint -q /mnt/disks/meili || mount -o discard,defaults /dev/disk/by-id/google-meilisearch-data /mnt/disks/meili'
            ExecStartPre=-/usr/bin/docker rm -f meilisearch
            ExecStart=/usr/bin/docker run --rm --name meilisearch -p 7700:7700 -v /mnt/disks/meili:/meili_data --env-file /etc/meilisearch.env getmeili/meilisearch:${var.meilisearch_version}
            ExecStop=/usr/bin/docker stop meilisearch
            Restart=always
            RestartSec=5

            [Install]
            WantedBy=multi-user.target
      runcmd:
        - systemctl daemon-reload
        - systemctl enable --now meilisearch.service
    EOT
  }

  // COS images are replaced, not patched in place; changing user-data requires a restart anyway.
  allow_stopping_for_update = true

  // `gcloud compute ssh` adds keys to instance metadata; don't strip them on apply.
  lifecycle {
    ignore_changes = [metadata["ssh-keys"]]
  }
}

output "meilisearch_url" {
  value = "http://${google_compute_instance.meilisearch.network_interface[0].network_ip}:7700"
}
