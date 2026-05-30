package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func writeEmailConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "email.json")
	if err := ioutil.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadEmailConfigCloudflare(t *testing.T) {
	path := writeEmailConfig(t, `{
		"provider": "cloudflare",
		"base_url": "https://slack-archive.persistent.info",
		"archive_from_email": "archive@slack-archive.persistent.info",
		"admin_from_email": "admin@slack-archive.persistent.info",
		"admin_to_email": "mihai@example.com",
		"cloudflare_account_id": "account123",
		"cloudflare_api_token": "token123"
	}`)
	config, err := loadEmailConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Provider != EmailProviderCloudflare {
		t.Fatalf("Provider = %q, want %q", config.Provider, EmailProviderCloudflare)
	}
}

func TestLoadEmailConfigAppEngine(t *testing.T) {
	path := writeEmailConfig(t, `{
		"provider": "appengine",
		"base_url": "https://slack-archive.appspot.com",
		"archive_from_email": "archive@slack-archive.appspotmail.com",
		"admin_from_email": "admin@slack-archive.appspotmail.com",
		"admin_to_email": "mihai@example.com"
	}`)
	config, err := loadEmailConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Provider != EmailProviderAppEngine {
		t.Fatalf("Provider = %q, want %q", config.Provider, EmailProviderAppEngine)
	}
}

func TestLoadEmailConfigMissingCloudflareCredentials(t *testing.T) {
	path := writeEmailConfig(t, `{
		"provider": "cloudflare",
		"base_url": "https://slack-archive.persistent.info",
		"archive_from_email": "archive@slack-archive.persistent.info",
		"admin_from_email": "admin@slack-archive.persistent.info",
		"admin_to_email": "mihai@example.com"
	}`)
	if _, err := loadEmailConfig(path); err == nil {
		t.Fatal("loadEmailConfig succeeded, want error")
	}
}

func TestCloudflareEmailSenderSendsRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, "POST"; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.URL.Path, "/client/v4/accounts/account123/email/sending/send"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer token123"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		from, ok := body["from"].(map[string]interface{})
		if !ok {
			t.Fatalf("from = %#v, want object", body["from"])
		}
		if got, want := from["address"], "archive@example.com"; got != want {
			t.Fatalf("from.address = %q, want %q", got, want)
		}
		if got, want := from["name"], "Team Slack Archive"; got != want {
			t.Fatalf("from.name = %q, want %q", got, want)
		}
		if got, want := body["subject"], "#tech Archive"; got != want {
			t.Fatalf("subject = %q, want %q", got, want)
		}
		headers, ok := body["headers"].(map[string]interface{})
		if !ok {
			t.Fatalf("headers = %#v, want object", body["headers"])
		}
		if got, want := headers["X-Slack-Archive-Idempotency-Key"], "archive:U123:channel:C123:user@example.com:2026-05-17"; got != want {
			t.Fatalf("idempotency header = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"errors":[],"messages":[],"result":{"delivered":["user@example.com"],"queued":[],"permanent_bounces":[]}}`))
	}))
	defer server.Close()

	sender := NewCloudflareEmailSender("account123", "token123", server.Client())
	sender.endpoint = server.URL + "/client/v4/accounts/account123/email/sending/send"
	result, err := sender.Send(context.Background(), EmailMessage{
		From:           "Team Slack Archive <archive@example.com>",
		To:             []string{"user@example.com"},
		Subject:        "#tech Archive",
		HTMLBody:       "<p>hello</p>",
		IdempotencyKey: "archive:U123:channel:C123:user@example.com:2026-05-17",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Delivered) != 1 || result.Delivered[0] != "user@example.com" {
		t.Fatalf("Delivered = %#v, want user@example.com", result.Delivered)
	}
}

func TestCloudflareEmailSenderAcceptsQueued(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"errors":[],"messages":[],"result":{"delivered":[],"queued":["user@example.com"],"permanent_bounces":[]}}`))
	}))
	defer server.Close()

	sender := NewCloudflareEmailSender("account123", "token123", server.Client())
	sender.endpoint = server.URL
	result, err := sender.Send(context.Background(), EmailMessage{
		From:     "archive@example.com",
		To:       []string{"user@example.com"},
		Subject:  "Archive",
		HTMLBody: "<p>hello</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Queued) != 1 || result.Queued[0] != "user@example.com" {
		t.Fatalf("Queued = %#v, want user@example.com", result.Queued)
	}
}

func TestCloudflareEmailSenderReportsPermanentBounces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"errors":[],"messages":[],"result":{"delivered":[],"queued":[],"permanent_bounces":["user@example.com"]}}`))
	}))
	defer server.Close()

	sender := NewCloudflareEmailSender("account123", "token123", server.Client())
	sender.endpoint = server.URL
	result, err := sender.Send(context.Background(), EmailMessage{
		From:     "archive@example.com",
		To:       []string{"user@example.com"},
		Subject:  "Archive",
		HTMLBody: "<p>hello</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PermanentBounces) != 1 || result.PermanentBounces[0] != "user@example.com" {
		t.Fatalf("PermanentBounces = %#v, want user@example.com", result.PermanentBounces)
	}
}

func TestCloudflareEmailSenderSurfacesErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"success":false,"errors":[{"code":10001,"message":"email.sending.error.invalid_request_schema"}],"messages":[],"result":null}`))
	}))
	defer server.Close()

	sender := NewCloudflareEmailSender("account123", "token123", server.Client())
	sender.endpoint = server.URL
	_, err := sender.Send(context.Background(), EmailMessage{
		From:     "archive@example.com",
		To:       []string{"user@example.com"},
		Subject:  "Archive",
		HTMLBody: "<p>hello</p>",
	})
	if err == nil {
		t.Fatal("Send succeeded, want error")
	}
}
