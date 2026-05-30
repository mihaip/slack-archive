# Slack Archive

Service for doing "off-site" archive of all your communications on Slack teams. Main use-case is for getting Slack messages into Gmail's history, so that you can search it alongside your email.

## Running Locally

  1. Install Go 1.25 and a current [Google Cloud CLI](https://cloud.google.com/sdk/docs/install).
  2. Install the App Engine local development server component:
     ```sh
     gcloud components install app-engine-python
     ```
  3. Authenticate if you need local access to Google Cloud services:
     ```sh
     gcloud auth login
     gcloud auth application-default login
     ```
  4. Create configuration files in the `config` directory, based on the sample files that are already there:
      - Create `slack-oauth.json` (you'll need to [register a new app](https://api.slack.com/applications/new) with Slack)
      - `session.json` (with randomly-generated keys)
      - `files.json` (another randomly-generated key)
      - `email.json` (see below for Cloudflare Email Service details)
  5. Run the local App Engine development server:
     ```sh
     ./dev.sh
     ```

The server can the be accessed at [http://localhost:8080/](http://localhost:8080/).

The local development server simulates App Engine bundled services such as
Datastore, Memcache, and Task Queues. App Engine Mail is a no-op locally unless
you pass SMTP options or `--enable_sendmail=yes` to `dev_appserver.py`. The
Cloudflare email provider can send real email when configured with production
credentials.

## Deploying to App Engine

```
./deploy.sh
```

`deploy.sh` deploys `app.yaml` and `queue.yaml`. Deploy `cron.yaml` separately
only when cron configuration changes.

## Cloudflare Email Sending

The app sends archive emails through Cloudflare Email Sending by default. The App Engine Mail API is still supported as a rollback provider via `config/email.json`.

To configure Cloudflare:

  1. In Cloudflare, enable Email Sending for `slack-archive.persistent.info` under Compute > Email Service > Email Sending.
  2. Create a [Cloudflare API token](https://dash.cloudflare.com/profile/api-tokens) with write permission to send email for the account.
  3. Copy `app/config/email.json.SAMPLE` to `app/config/email.json` and set:
     * `provider` to `cloudflare`
     * `base_url` to `https://slack-archive.persistent.info`
     * `archive_from_email` to `archive@slack-archive.persistent.info`
     * `admin_from_email` to `admin@slack-archive.persistent.info`
     * `cloudflare_account_id` and `cloudflare_api_token`
  4. Keep the App Engine custom domain DNS record separate and DNS-only in Cloudflare:
     * `slack-archive.persistent.info CNAME ghs.googlehosted.com`

To roll back email sending without changing code, set `provider` to `appengine` in `app/config/email.json`, set the sender addresses back to App Engine Mail-compatible `appspotmail.com` addresses, and redeploy.
