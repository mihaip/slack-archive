# Slack Archive

Service for doing "off-site" archive of all your communications on Slack teams. Main use-case is for getting Slack messages into Gmail's history, so that you can search it alongside your email.

## Running Locally

  1. [Install the Go App Engine SDK](https://developers.google.com/appengine/downloads#Google_App_Engine_SDK_for_Go).
  2. Install the depencies:
     * `go get github.com/gorilla/mux`
     * `go get github.com/gorilla/sessions`
     * `go get github.com/nlopes/slack`
  3. Create `slack-oauth.json` (you'll need to [register a new app](https://api.slack.com/applications/new) with Slack), `session.json` (with randomly-generated keys) and `files.json` files in the `config` directory, based on the sample files that are already there.
  4. Make sure that `PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION` is set to `python`.
  5. Run: `dev_appserver.py --enable_sendmail=yes app`

The server can the be accessed at [http://localhost:8080/](http://localhost:8080/).

## Deploying to App Engine

```
goapp deploy app
```
