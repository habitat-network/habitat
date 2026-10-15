// Cloud Build trigger that builds `cmd/pear/Dockerfile` on push to main and
// redeploys Cloud Run services `habitat` and `pear` via `gcloud run services update`.
// Imported from GCP: projects/virtual-charger-477003-g0/locations/us-west1/triggers/bfc92f56-a0c0-49a9-a750-02e3e83ba954
// (_TRIGGER_ID substitution 97bca61b-dd17-402d-9912-abb57b201ad6 is the legacy ID
// referenced in main.tf).
//
// The GitHub connection (projects/.../connections/github) and its OAuth secret are
// managed outside Terraform (Cloud Build 2nd-gen OAuth handshake).

resource "google_cloudbuild_trigger" "pear" {
  name               = "rmgpgab-habitat-us-west1-habitat-network-habitat--maxbw"
  description        = "Build and deploy to Cloud Run service habitat on push to \"^main$\""
  location           = var.region
  project            = var.project
  service_account    = "projects/virtual-charger-477003-g0/serviceAccounts/953995456319-compute@developer.gserviceaccount.com"
  include_build_logs = "INCLUDE_BUILD_LOGS_WITH_STATUS"
  included_files     = ["internal/**", "cmd/pear/**", "go.mod", "go.sum"]

  substitutions = {
    _AR_HOSTNAME    = "us-west1-docker.pkg.dev"
    _AR_PROJECT_ID  = "virtual-charger-477003-g0"
    _AR_REPOSITORY  = "cloud-run-source-deploy"
    _DEPLOY_REGION  = "us-west1"
    _PLATFORM       = "managed"
    _SERVICE_NAME   = "habitat"
    _SERVICE_NAME_2 = "pear"
    _TRIGGER_ID     = "97bca61b-dd17-402d-9912-abb57b201ad6"
  }

  tags = ["gcp-cloud-build-deploy-cloud-run", "gcp-cloud-build-deploy-cloud-run-managed", "habitat"]

  approval_config {
    approval_required = false
  }

  build {
    images  = ["$_AR_HOSTNAME/$_AR_PROJECT_ID/$_AR_REPOSITORY/$REPO_NAME/$_SERVICE_NAME:$COMMIT_SHA"]
    tags    = ["gcp-cloud-build-deploy-cloud-run", "gcp-cloud-build-deploy-cloud-run-managed", "habitat"]
    timeout = "600s"

    substitutions = {
      _AR_HOSTNAME   = "us-west1-docker.pkg.dev"
      _AR_PROJECT_ID = "virtual-charger-477003-g0"
      _AR_REPOSITORY = "cloud-run-source-deploy"
      _DEPLOY_REGION = "us-west1"
      _PLATFORM      = "managed"
      _SERVICE_NAME  = "habitat"
      _TRIGGER_ID    = "97bca61b-dd17-402d-9912-abb57b201ad6"
    }

    options {
      logging             = "CLOUD_LOGGING_ONLY"
      substitution_option = "ALLOW_LOOSE"
    }

    step {
      name = "gcr.io/cloud-builders/docker"
      id   = "Build"
      args = ["build", "-t", "$_AR_HOSTNAME/$_AR_PROJECT_ID/$_AR_REPOSITORY/$REPO_NAME/$_SERVICE_NAME:$COMMIT_SHA", ".", "-f", "cmd/pear/Dockerfile"]
      env  = ["DOCKER_BUILDKIT=1"]
    }

    step {
      name = "gcr.io/cloud-builders/docker"
      id   = "Push"
      args = ["push", "$_AR_HOSTNAME/$_AR_PROJECT_ID/$_AR_REPOSITORY/$REPO_NAME/$_SERVICE_NAME:$COMMIT_SHA"]
    }

    // Point the Cloud Run jobs (see main.tf) at the new image, then apply migrations with
    // pear-migrate before any service revision takes traffic. A failed migration fails the
    // build, so the services keep running the previous revision.
    step {
      name       = "gcr.io/google.com/cloudsdktool/cloud-sdk:slim"
      id         = "UpdateJobs"
      entrypoint = "bash"
      args       = ["-c", "for job in pear-migrate pear-run; do gcloud run jobs update $$job --image=$_AR_HOSTNAME/$_AR_PROJECT_ID/$_AR_REPOSITORY/$REPO_NAME/$_SERVICE_NAME:$COMMIT_SHA --region=$_DEPLOY_REGION --quiet || exit 1; done"]
    }

    step {
      name       = "gcr.io/google.com/cloudsdktool/cloud-sdk:slim"
      id         = "Migrate"
      entrypoint = "gcloud"
      args       = ["run", "jobs", "execute", "pear-migrate", "--region=$_DEPLOY_REGION", "--wait", "--quiet"]
    }

    step {
      name       = "gcr.io/google.com/cloudsdktool/cloud-sdk:slim"
      id         = "Deploy"
      entrypoint = "gcloud"
      args       = ["run", "services", "update", "$_SERVICE_NAME", "--platform=managed", "--image=$_AR_HOSTNAME/$_AR_PROJECT_ID/$_AR_REPOSITORY/$REPO_NAME/$_SERVICE_NAME:$COMMIT_SHA", "--labels=managed-by=gcp-cloud-build-deploy-cloud-run,commit-sha=$COMMIT_SHA,gcb-build-id=$BUILD_ID,gcb-trigger-id=$_TRIGGER_ID", "--region=$_DEPLOY_REGION", "--quiet"]
    }

    step {
      name       = "gcr.io/google.com/cloudsdktool/cloud-sdk:slim"
      id         = "Deploy2"
      entrypoint = "gcloud"
      args       = ["run", "services", "update", "$_SERVICE_NAME_2", "--platform=managed", "--image=$_AR_HOSTNAME/$_AR_PROJECT_ID/$_AR_REPOSITORY/$REPO_NAME/$_SERVICE_NAME:$COMMIT_SHA", "--labels=managed-by=gcp-cloud-build-deploy-cloud-run,commit-sha=$COMMIT_SHA,gcb-build-id=$BUILD_ID,gcb-trigger-id=$_TRIGGER_ID", "--region=$_DEPLOY_REGION", "--quiet"]
    }
  }

  repository_event_config {
    repository = "projects/virtual-charger-477003-g0/locations/us-west1/connections/github/repositories/habitat-network-habitat"

    push {
      branch = "^main$"
    }
  }
}
