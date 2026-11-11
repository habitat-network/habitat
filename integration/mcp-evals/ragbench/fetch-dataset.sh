#!/usr/bin/env bash
# Fetches the part of EnterpriseRAG-Bench that ragbench uses (the questions,
# the document index, and the Slack, Google Drive and GitHub documents; about
# 2 GB) into a directory, at a pinned commit so runs are comparable.
#
# The dataset asks to be kept out of training corpora, so it's never
# committed here: fetch it somewhere outside the repository.
#
#   fetch-dataset.sh [dir]     default: ~/.cache/habitat/enterprise-rag-bench
set -euo pipefail

repo=https://github.com/onyx-dot-app/EnterpriseRAG-Bench
commit=d36685e273713975ee20299bbf1ab64165575b3c
dir=${1:-$HOME/.cache/habitat/enterprise-rag-bench}

if [ ! -d "$dir/.git" ]; then
  mkdir -p "$dir"
  git -C "$dir" init --quiet
  git -C "$dir" remote add origin "$repo"
fi
git -C "$dir" sparse-checkout set \
  generated_data/sources/slack \
  generated_data/sources/google_drive \
  generated_data/sources/github
git -C "$dir" fetch --depth 1 --filter=blob:none origin "$commit"
git -C "$dir" checkout --quiet FETCH_HEAD

echo "Dataset at $dir. Run: go run ./ragbench retrieval -dataset $dir"
