# Slack Archive

Service for doing "off-site" archive of all your communications on Slack teams. Main use-case is for getting Slack messages into Gmail's history, so that you can search it alongside your email.

## Running Locally

  1. [Install the Go App Engine SDK](https://developers.google.com/appengine/downloads#Google_App_Engine_SDK_for_Go).
  2. Install the depencies:
     * `go get github.com/gorilla/mux`
     * `go get github.com/gorilla/sessions`
     * `go get github.com/slack-go/slack`
  3. Create `slack-oauth.json` (you'll need to [register a new app](https://api.slack.com/applications/new) with Slack), `session.json` (with randomly-generated keys), `files.json` and `email.json` files in the `config` directory, based on the sample files that are already there.
  4. Make sure that `PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION` is set to `python`.
  5. Run: `dev_appserver.py --enable_sendmail=yes app`

The server can the be accessed at [http://localhost:8080/](http://localhost:8080/).

## Deploying to App Engine

```
./deploy.sh
```

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
