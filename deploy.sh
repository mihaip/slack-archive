#!/bin/sh

cd app
gcloud app deploy --project slack-archive app.yaml queue.yaml
