#!/bin/sh

set -e

cd app
python3 "$(gcloud info --format='value(installation.sdk_root)')/bin/dev_appserver.py" \
  --application=slack-archive \
  --port="${PORT:-8080}" \
  app.yaml
