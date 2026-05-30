package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	appenginelog "google.golang.org/appengine/v2/log"
	appenginemail "google.golang.org/appengine/v2/mail"
	"google.golang.org/appengine/v2/urlfetch"
)

const (
	EmailProviderAppEngine  = "appengine"
	EmailProviderCloudflare = "cloudflare"
)

type EmailConfig struct {
	Provider            string `json:"provider"`
	BaseUrl             string `json:"base_url"`
	ArchiveFromEmail    string `json:"archive_from_email"`
	AdminFromEmail      string `json:"admin_from_email"`
	AdminToEmail        string `json:"admin_to_email"`
	CloudflareAccountID string `json:"cloudflare_account_id"`
	CloudflareAPIToken  string `json:"cloudflare_api_token"`
}

type EmailMessage struct {
	From           string
	To             []string
	Subject        string
	TextBody       string
	HTMLBody       string
	IdempotencyKey string
}

type EmailSendResult struct {
	Provider         string
	MessageID        string
	Delivered        []string
	Queued           []string
	PermanentBounces []string
}

type EmailSender interface {
	Send(context.Context, EmailMessage) (EmailSendResult, error)
}

type AppEngineEmailSender struct{}

func (s *AppEngineEmailSender) Send(c context.Context, message EmailMessage) (EmailSendResult, error) {
	appEngineMessage := &appenginemail.Message{
		Sender:   message.From,
		To:       message.To,
		Subject:  message.Subject,
		Body:     message.TextBody,
		HTMLBody: message.HTMLBody,
	}
	err := appenginemail.Send(c, appEngineMessage)
	return EmailSendResult{Provider: EmailProviderAppEngine}, err
}

type CloudflareEmailSender struct {
	accountID string
	apiToken  string
	client    *http.Client
	endpoint  string
}

func NewCloudflareEmailSender(accountID string, apiToken string, client *http.Client) *CloudflareEmailSender {
	return &CloudflareEmailSender{
		accountID: accountID,
		apiToken:  apiToken,
		client:    client,
		endpoint: fmt.Sprintf(
			"https://api.cloudflare.com/client/v4/accounts/%s/email/sending/send",
			url.PathEscape(accountID)),
	}
}

func (s *CloudflareEmailSender) Send(c context.Context, message EmailMessage) (EmailSendResult, error) {
	body := map[string]interface{}{
		"from":    cloudflareEmailAddress(message.From),
		"to":      message.To,
		"subject": message.Subject,
	}
	if message.HTMLBody != "" {
		body["html"] = message.HTMLBody
	}
	if message.TextBody != "" {
		body["text"] = message.TextBody
	}
	if message.IdempotencyKey != "" {
		body["headers"] = map[string]string{
			"X-Slack-Archive-Idempotency-Key": message.IdempotencyKey,
		}
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return EmailSendResult{}, err
	}
	req, err := http.NewRequest("POST", s.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return EmailSendResult{}, err
	}
	req = req.WithContext(c)
	req.Header.Set("Authorization", "Bearer "+s.apiToken)
	req.Header.Set("Content-Type", "application/json")
	client := s.client
	if client == nil {
		client = AppEngineHTTPClient(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		return EmailSendResult{}, err
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return EmailSendResult{}, err
	}
	var sendResp cloudflareSendResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &sendResp); err != nil {
			return EmailSendResult{}, fmt.Errorf("could not parse Cloudflare response: %s", err.Error())
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(sendResp.Errors) > 0 {
			return EmailSendResult{}, fmt.Errorf("Cloudflare send failed: HTTP %d: %s", resp.StatusCode, cloudflareErrorsString(sendResp.Errors))
		}
		return EmailSendResult{}, fmt.Errorf("Cloudflare send failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if !sendResp.Success {
		return EmailSendResult{}, fmt.Errorf("Cloudflare send failed: %s", cloudflareErrorsString(sendResp.Errors))
	}
	return EmailSendResult{
		Provider:         EmailProviderCloudflare,
		Delivered:        sendResp.Result.Delivered,
		Queued:           sendResp.Result.Queued,
		PermanentBounces: sendResp.Result.PermanentBounces,
	}, nil
}

type cloudflareSendResponse struct {
	Success bool              `json:"success"`
	Errors  []cloudflareError `json:"errors"`
	Result  struct {
		Delivered        []string `json:"delivered"`
		Queued           []string `json:"queued"`
		PermanentBounces []string `json:"permanent_bounces"`
	} `json:"result"`
}

type cloudflareError struct {
	Code    interface{} `json:"code"`
	Message string      `json:"message"`
}

func cloudflareEmailAddress(address string) interface{} {
	parsedAddress, err := mail.ParseAddress(address)
	if err != nil || parsedAddress.Name == "" {
		return address
	}
	return map[string]string{
		"address": parsedAddress.Address,
		"name":    parsedAddress.Name,
	}
}

func cloudflareErrorsString(errors []cloudflareError) string {
	if len(errors) == 0 {
		return "unknown error"
	}
	parts := make([]string, 0, len(errors))
	for _, e := range errors {
		parts = append(parts, fmt.Sprintf("%v: %s", e.Code, e.Message))
	}
	return strings.Join(parts, "; ")
}

func initEmailConfig() EmailConfig {
	config, err := loadEmailConfig("config/email.json")
	if err != nil {
		log.Panicf("Could not load email config: %s", err.Error())
	}
	return config
}

func loadEmailConfig(path string) (EmailConfig, error) {
	configBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return EmailConfig{}, err
	}
	var config EmailConfig
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return EmailConfig{}, err
	}
	if config.Provider == "" {
		return EmailConfig{}, errors.New("provider is required")
	}
	if config.BaseUrl == "" {
		return EmailConfig{}, errors.New("base_url is required")
	}
	if config.ArchiveFromEmail == "" {
		return EmailConfig{}, errors.New("archive_from_email is required")
	}
	if config.AdminFromEmail == "" {
		return EmailConfig{}, errors.New("admin_from_email is required")
	}
	if config.AdminToEmail == "" {
		return EmailConfig{}, errors.New("admin_to_email is required")
	}
	if config.Provider == EmailProviderCloudflare && config.CloudflareAccountID == "" {
		return EmailConfig{}, errors.New("cloudflare_account_id is required for cloudflare provider")
	}
	if config.Provider == EmailProviderCloudflare && config.CloudflareAPIToken == "" {
		return EmailConfig{}, errors.New("cloudflare_api_token is required for cloudflare provider")
	}
	if config.Provider != EmailProviderCloudflare && config.Provider != EmailProviderAppEngine {
		return EmailConfig{}, fmt.Errorf("unknown email provider %q", config.Provider)
	}
	return config, nil
}

func initEmailSender(config EmailConfig) EmailSender {
	switch config.Provider {
	case EmailProviderAppEngine:
		return &AppEngineEmailSender{}
	case EmailProviderCloudflare:
		return NewCloudflareEmailSender(config.CloudflareAccountID, config.CloudflareAPIToken, nil)
	default:
		log.Panicf("Unknown email provider %q", config.Provider)
		return nil
	}
}

func SendEmail(c context.Context, message EmailMessage) error {
	result, err := emailSender.Send(c, message)
	if err != nil {
		return err
	}
	logEmailSend(c, result, message)
	return nil
}

func logEmailSend(c context.Context, result EmailSendResult, message EmailMessage) {
	appenginelog.Infof(c, "Sent email provider=%s message_id=%s delivered=%s queued=%s permanent_bounces=%s to=%s subject=%q",
		result.Provider,
		result.MessageID,
		strings.Join(result.Delivered, ","),
		strings.Join(result.Queued, ","),
		strings.Join(result.PermanentBounces, ","),
		strings.Join(message.To, ","),
		message.Subject)
}

func AppEngineHTTPClient(c context.Context) *http.Client {
	return &http.Client{Transport: &urlfetch.Transport{Context: c}}
}
