package main

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/delay"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/mail"
	"google.golang.org/appengine/urlfetch"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/slack-go/slack"
)

var router *mux.Router
var slackOAuthConfig OAuthConfig
var timezones Timezones
var sessionStore *sessions.CookieStore
var sessionConfig SessionConfig
var styles map[string]template.CSS
var templates map[string]*Template
var fileUrlRefEncryptionKey []byte
var emojiByShortName map[string]*Emoji

func main() {
	styles = loadStyles()
	templates = loadTemplates()
	timezones = initTimezones()
	sessionStore, sessionConfig = initSession()
	slackOAuthConfig = initSlackOAuthConfig()
	fileUrlRefEncryptionKey = loadFileUrlRefEncryptionKey()
	emojiByShortName = loadEmoji()

	router = mux.NewRouter()
	router.Handle("/", AppHandler(indexHandler)).Name("index")

	router.Handle("/session/sign-in", AppHandler(signInHandler)).Name("sign-in").Methods("POST")
	router.Handle("/session/sign-out", AppHandler(signOutHandler)).Name("sign-out").Methods("POST")
	router.Handle("/slack/callback", AppHandler(slackOAuthCallbackHandler)).Name("slack-callback")

	router.Handle("/archive/send", SignedInAppHandler(sendArchiveHandler)).Name("send-archive").Methods("POST")
	router.Handle("/archive/cron", AppHandler(archiveCronHandler))
	router.Handle("/archive/conversation/send", SignedInAppHandler(sendConversationArchiveHandler)).Name("send-conversation-archive").Methods("POST")
	router.Handle("/archive/conversation/{type}/{ref}", SignedInAppHandler(conversationArchiveHandler)).Name("conversation-archive")
	router.Handle("/archive/file-thumbnail/{ref}", AppHandler(archiveFileThumbnailHandler)).Name("archive-file-thumbnail")

	router.Handle("/account/settings", SignedInAppHandler(settingsHandler)).Name("settings").Methods("GET")
	router.Handle("/account/settings", SignedInAppHandler(saveSettingsHandler)).Name("save-settings").Methods("POST")
	router.Handle("/account/delete", SignedInAppHandler(deleteAccountHandler)).Name("delete-account").Methods("POST")

	http.Handle("/", router)

	appengine.Main()
}

func indexHandler(w http.ResponseWriter, r *http.Request) *AppError {
	session, _ := sessionStore.Get(r, sessionConfig.CookieName)
	userId, ok := session.Values[sessionConfig.UserIdKey].(string)
	if !ok {
		data := map[string]interface{}{
			"ContinueUrl": r.FormValue("continue_url"),
		}
		return templates["index-signed-out"].Render(w, data)
	}
	c := appengine.NewContext(r)
	account, err := getAccount(c, userId)
	if account == nil {
		// Can't look up the account, session cookie must be invalid, clear it.
		session.Options.MaxAge = -1
		session.Save(r, w)
		return RedirectToRoute("index")
	}
	if err != nil {
		return InternalError(err, "Could not look up account")
	}

	slackClient := account.NewSlackClient(c)

	user, err := slackClient.GetUserInfo(account.SlackUserId)
	if err != nil {
		return SlackFetchError(err, "user")
	}
	team, err := slackClient.GetTeamInfo()
	if err != nil {
		return SlackFetchError(err, "team")
	}
	conversations, err := getConversations(slackClient, account)
	if err != nil {
		return SlackFetchError(err, "conversations")
	}

	emailAddress, err := account.GetDigestEmailAddress(slackClient)
	if err != nil {
		return SlackFetchError(err, "emails")
	}

	var settingsSummary = map[string]interface{}{
		"EmailAddress":      emailAddress,
		"ConversationCount": len(conversations.AllConversations),
	}
	var data = map[string]interface{}{
		"User":            user,
		"Team":            team,
		"Conversations":   conversations,
		"SettingsSummary": settingsSummary,
	}
	return templates["index"].Render(w, data, &AppSignedInState{
		Account:        account,
		SlackClient:    slackClient,
		session:        session,
		responseWriter: w,
		request:        r,
	})
}

func signInHandler(w http.ResponseWriter, r *http.Request) *AppError {
	authCodeUrl, _ := url.Parse("https://slack.com/oauth/authorize")
	authCodeUrlQuery := authCodeUrl.Query()
	authCodeUrlQuery.Set("client_id", slackOAuthConfig.ClientId)
	authCodeUrlQuery.Set("scope", strings.Join([]string{
		// Basic user info
		"users:read",
		// User email address
		// (after https://api.slack.com/changelog/2017-04-narrowing-email-access)
		"users:read.email",
		// Team info
		"team:read",
		// Channel archive
		"channels:read channels:history",
		// Private channel archive
		"groups:read groups:history",
		// Direct message archive
		"im:read im:history",
		// Multi-party direct mesage archive
		"mpim:read mpim:history",
		// Read file thumbnail
		"files:read",
		// Read custom emoji
		"emoji:read",
	}, " "))
	redirectUrlString, _ := AbsoluteRouteUrl("slack-callback")
	redirectUrl, _ := url.Parse(redirectUrlString)
	if continueUrl := r.FormValue("continue_url"); continueUrl != "" {
		redirectUrlQuery := redirectUrl.Query()
		redirectUrlQuery.Set("continue_url", continueUrl)
		redirectUrl.RawQuery = redirectUrlQuery.Encode()
	}
	authCodeUrlQuery.Set("redirect_uri", redirectUrl.String())
	authCodeUrl.RawQuery = authCodeUrlQuery.Encode()
	return RedirectToUrl(authCodeUrl.String())
}

func signOutHandler(w http.ResponseWriter, r *http.Request) *AppError {
	session, _ := sessionStore.Get(r, sessionConfig.CookieName)
	session.Options.MaxAge = -1
	session.Save(r, w)
	return RedirectToRoute("index")
}

func slackOAuthCallbackHandler(w http.ResponseWriter, r *http.Request) *AppError {
	c := appengine.NewContext(r)
	httpClient := urlfetch.Client(c)

	code := r.FormValue("code")
	redirectUrl := AbsolutePathUrl(r.URL.Path)
	token, _, err := slack.GetOAuthToken(
		httpClient, slackOAuthConfig.ClientId, slackOAuthConfig.ClientSecret, code,
		redirectUrl)
	if err != nil {
		return InternalError(err, "Could not exchange OAuth code")
	}

	slackClient := slack.New(token)
	authTest, err := slackClient.AuthTest()
	if err != nil {
		return SlackFetchError(err, "user")
	}

	allowedTeams := []string{
		"Partyslack",
		"Tailscale",
		"More Partier More Chattier",
		"Partiest Chattiest",
		"DanceDeets",
		"Spring '17 Babies",
		"Medallandia",
		"Parparitaville",
		"AppKit Abusers",
	}
	isAllowedTeam := false
	for _, allowedTeam := range allowedTeams {
		if authTest.Team == allowedTeam {
			isAllowedTeam = true
			break
		}
	}
	if !isAllowedTeam {
		log.Warningf(c, "Non-whitelisted team %s used", authTest.Team)
		return templates["team-not-on-whitelist"].Render(w, map[string]interface{}{})
	}

	account, err := getAccount(c, authTest.UserID)
	if err != nil && err != datastore.ErrNoSuchEntity {
		return InternalError(err, "Could not look up user")
	}
	if account == nil {
		timezoneName := ""
		if user, err := slackClient.GetUserInfo(authTest.UserID); err == nil && len(user.TZ) > 0 {
			if _, err := time.LoadLocation(user.TZ); err == nil {
				timezoneName = user.TZ
			}
		}
		account = &Account{
			SlackUserId:   authTest.UserID,
			SlackTeamName: authTest.Team,
			SlackTeamUrl:  authTest.URL,
			TimezoneName:  timezoneName,
		}
	}
	account.ApiToken = token
	// Persist the default email address now, both to avoid additional lookups
	// later and to have a way to contact the user if they ever revoke their
	// OAuth token.
	emailAddress, err := account.GetDigestEmailAddress(slackClient)
	if err == nil && len(emailAddress) > 0 {
		account.DigestEmailAddress = emailAddress
	}
	err = account.Put(c)
	if err != nil {
		return InternalError(err, "Could not save user")
	}

	session, _ := sessionStore.Get(r, sessionConfig.CookieName)
	session.Values[sessionConfig.UserIdKey] = account.SlackUserId
	session.Save(r, w)
	continueUrl := r.FormValue("continue_url")
	if continueUrl != "" {
		continueUrlParsed, err := url.Parse(continueUrl)
		if err != nil || continueUrlParsed.Host != r.URL.Host {
			continueUrl = ""
		}
	}
	if continueUrl == "" {
		indexUrl, _ := router.Get("index").URL()
		continueUrl = indexUrl.String()
	}
	return RedirectToUrl(continueUrl)
}

func conversationArchiveHandler(w http.ResponseWriter, r *http.Request, state *AppSignedInState) *AppError {
	vars := mux.Vars(r)
	conversationType := vars["type"]
	ref := vars["ref"]
	conversation, err := getConversationFromRef(conversationType, ref, state.SlackClient)
	if err != nil {
		return SlackFetchError(err, "conversation")
	}

	archive, err := newConversationArchive(conversation, state.SlackClient, state.Account, r.FormValue("dev") == "1")
	if err != nil {
		return SlackFetchError(err, "archive")
	}

	var data = map[string]interface{}{
		"Conversation":        conversation,
		"ConversationType":    conversationType,
		"ConversationRef":     ref,
		"ConversationArchive": archive,
	}
	return templates["conversation-archive-page"].Render(w, data, state)
}

func sendArchiveHandler(w http.ResponseWriter, r *http.Request, state *AppSignedInState) *AppError {
	c := appengine.NewContext(r)
	sentCount, err := sendArchive(state.Account, c)
	if err != nil {
		return InternalError(err, "Could not send archive")
	}
	if sentCount > 0 {
		if sentCount == 1 {
			state.AddFlash("Emailed 1 archive!")
		} else {
			state.AddFlash(fmt.Sprintf("Emailed %d archives!", sentCount))
		}
	} else {
		state.AddFlash("No archives were sent, they were either all empty or disabled.")
	}
	return RedirectToRoute("index")
}

func archiveCronHandler(w http.ResponseWriter, r *http.Request) *AppError {
	c := appengine.NewContext(r)
	accounts, err := getAllAccounts(c)
	if err != nil {
		return InternalError(err, "Could not look up accounts")
	}
	for _, account := range accounts {
		now := time.Now().In(account.TimezoneLocation)
		oneHourAgo := now.Add(-time.Hour)
		if now.Day() != oneHourAgo.Day() {
			log.Infof(c, "Enqueing task for %s...", account.SlackUserId)
			sendArchiveFunc.Call(c, account.SlackUserId)
		}
	}
	fmt.Fprint(w, "Done")
	return nil
}

var sendArchiveFunc = delay.Func(
	"sendArchive",
	func(c context.Context, slackUserId string) error {
		log.Infof(c, "Sending digest for %s...", slackUserId)
		account, err := getAccount(c, slackUserId)
		if err != nil {
			log.Errorf(c, "  Error looking up account: %s", err.Error())
			return err
		}
		slackClient := account.NewSlackClient(c)
		conversations, err := getConversations(slackClient, account)
		if err != nil {
			log.Errorf(c, "  Error looking up conversations: %s", err.Error())
			if !appengine.IsDevAppServer() {
				sendArchiveErrorMail(err, c, slackUserId)
			}
			return err
		}
		if len(conversations.AllConversations) > 0 {
			for _, conversation := range conversations.AllConversations {
				conversationType, ref := conversation.ToRef()
				sendConversationArchiveFunc.Call(
					c, account.SlackUserId, conversationType, ref)
			}
			log.Infof(c, "  Enqueued %d conversation archives.", len(conversations.AllConversations))
		} else {
			log.Infof(c, "  Not sent, no conversations found.")
		}
		return nil
	})

var sendConversationArchiveFunc = delay.Func(
	"sendConversationArchive",
	func(c context.Context, slackUserId string, conversationType string, ref string) error {
		log.Infof(c, "Sending archive for %s conversation %s %s...",
			slackUserId, conversationType, ref)
		account, err := getAccount(c, slackUserId)
		if err != nil {
			log.Errorf(c, "  Error looking up account: %s", err.Error())
			return err
		}
		slackClient := account.NewSlackClient(c)
		conversation, err := getConversationFromRef(conversationType, ref, slackClient)
		if err != nil {
			log.Errorf(c, "  Error looking up conversation: %s", err.Error())
			if !appengine.IsDevAppServer() {
				sendArchiveErrorMail(err, c, slackUserId)
			}
			return err
		}
		sent, err := sendConversationArchive(conversation, account, c)
		if err != nil {
			log.Errorf(c, "  Error sending conversation archive: %s", err.Error())
			if !appengine.IsDevAppServer() {
				sendArchiveErrorMail(err, c, slackUserId)
			}
			return err
		}
		if sent {
			log.Infof(c, "  Sent!")
		} else {
			log.Infof(c, "  Not sent, archive was empty.")
		}
		return nil
	})

func sendArchive(account *Account, c context.Context) (int, error) {
	slackClient := account.NewSlackClient(c)
	conversations, err := getConversations(slackClient, account)
	if err != nil {
		return 0, err
	}
	sentCount := 0
	for _, conversation := range conversations.AllConversations {
		sent, err := sendConversationArchive(conversation, account, c)
		if err != nil {
			return sentCount, err
		}
		if sent {
			sentCount++
		}
	}
	return sentCount, nil
}

func sendArchiveErrorMail(e error, c context.Context, slackUserId string) {
	if appengine.IsTimeoutError(e) ||
		strings.Contains(e.Error(), "Canceled") ||
		strings.Contains(e.Error(), "context canceled") ||
		strings.Contains(e.Error(), "invalid security ticket") ||
		strings.Contains(e.Error(), "Call error 11") {
		// Ignore these errors, they are internal to App Engine.
		// Timeout error and "Canceled" may happen when a urlfetch is still
		// going on after the request timeout fires, but for us it happens
		// within a few seconds.
		// "invalid security ticket" may happen when using an App Engine context
		// after the HTTP request for it finishes, but we're not doing that.
		// Since delayed tasks will be retried if they return an error (and
		// these errors are transient), we don't want to know about them.
		return
	}
	errorMessage := &mail.Message{
		Sender:  "Slack Archive Admin <admin@slack-archive.appspotmail.com>",
		To:      []string{"mihai.parparita@gmail.com"},
		Subject: fmt.Sprintf("Slack Archive Send Error for %s", slackUserId),
		Body:    fmt.Sprintf("Error: %s", e),
	}
	err := mail.Send(c, errorMessage)
	if err != nil {
		log.Errorf(c, "Error %s sending error email.", err.Error())
	}
}

func sendConversationArchiveHandler(w http.ResponseWriter, r *http.Request, state *AppSignedInState) *AppError {
	conversationType := r.FormValue("conversation_type")
	ref := r.FormValue("conversation_ref")
	conversation, err := getConversationFromRef(conversationType, ref, state.SlackClient)
	if err != nil {
		return SlackFetchError(err, "conversation")
	}
	c := appengine.NewContext(r)
	sent, err := sendConversationArchive(conversation, state.Account, c)
	if err != nil {
		return InternalError(err, "Could not send conversation archive")
	}
	if sent {
		state.AddFlash("Emailed archive!")
	} else {
		state.AddFlash("No archive was sent, it was empty or disabled.")
	}
	return RedirectToRoute("conversation-archive", "type", conversationType, "ref", ref)
}

func sendConversationArchive(conversation Conversation, account *Account, c context.Context) (bool, error) {
	slackClient := account.NewSlackClient(c)
	emailAddress, err := account.GetDigestEmailAddress(slackClient)
	if err != nil {
		return false, err
	}
	if emailAddress == "disabled" {
		return false, nil
	}
	archive, err := newConversationArchive(conversation, slackClient, account, false)
	if err != nil {
		return false, err
	}
	if archive.Empty() {
		return false, nil
	}
	var data = map[string]interface{}{
		"ConversationArchive": archive,
	}
	var archiveHtml bytes.Buffer
	if err := templates["conversation-archive-email"].Execute(&archiveHtml, data); err != nil {
		return false, err
	}
	team, err := slackClient.GetTeamInfo()
	if err != nil {
		return false, err
	}
	sender := fmt.Sprintf(
		"%s Slack Archive <archive@slack-archive.appspotmail.com>", team.Name)
	archiveMessage := &mail.Message{
		Sender:   sender,
		To:       []string{emailAddress},
		Subject:  fmt.Sprintf("%s Archive", conversation.Name()),
		HTMLBody: archiveHtml.String(),
	}
	err = mail.Send(c, archiveMessage)
	return true, err
}

func archiveFileThumbnailHandler(w http.ResponseWriter, r *http.Request) *AppError {
	vars := mux.Vars(r)
	encodedRef := vars["ref"]
	ref, err := DecodeFileUrlRef(encodedRef)
	if err != nil {
		return BadRequest(err, "malformed ref")
	}

	c := appengine.NewContext(r)

	account, err := getAccount(c, ref.SlackUserId)
	if err != nil {
		return BadRequest(err, "no acccount")
	}

	slackClient := account.NewSlackClient(c)
	file, _, _, err := slackClient.GetFileInfo(ref.FileId, 0, 0)
	if err != nil {
		if slackErr, ok := err.(slack.SlackErrorResponse); ok && slackErr.Err == "hidden_by_limit" {
			return BadRequest(err, "tombstoned file")
		}
		return SlackFetchError(err, "file")
	}

	// We're displaying using the Thumb360 dimensions, but prefer the 720 data
	// (if available) for retina screens.
	url := file.Thumb720
	if url == "" {
		url = file.Thumb360
	}
	log.Infof(c, "Proxying %s for %s", url, ref.SlackUserId)
	ctx_with_timeout, _ := context.WithTimeout(c, time.Second*60)
	appengineTransport := &urlfetch.Transport{Context: ctx_with_timeout}
	cachingTransport := &CachingTransport{
		Transport: appengineTransport,
		Context:   ctx_with_timeout,
	}
	client := http.Client{Transport: cachingTransport}
	fileReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return InternalError(err, "could not create request")
	}
	fileReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", account.ApiToken))
	fileResp, err := client.Do(fileReq)
	if err != nil {
		return InternalError(err, "could not get file response")
	}
	copyHeaders := [...]string{
		"Cache-Control",
		"Content-Length",
		"Etag",
		"Expires",
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Content-Type",
		"Last-Modified",
	}
	for _, h := range copyHeaders {
		v, ok := fileResp.Header[h]
		if ok && len(v) == 1 {
			w.Header()[h] = v
		}
	}
	_, err = io.Copy(w, fileResp.Body)
	if err != nil {
		return InternalError(err, "could not copy response")
	}
	return nil
}

func settingsHandler(w http.ResponseWriter, r *http.Request, state *AppSignedInState) *AppError {
	account := state.Account
	emailAddress, err := account.GetDigestEmailAddress(state.SlackClient)
	if err != nil {
		return SlackFetchError(err, "emails")
	}
	user, err := state.SlackClient.GetUserInfo(account.SlackUserId)
	if err != nil {
		return SlackFetchError(err, "user")
	}
	var data = map[string]interface{}{
		"Account":             account,
		"User":                user,
		"AccountEmailAddress": emailAddress,
		"Timezones":           timezones,
	}
	return templates["settings"].Render(w, data, state)
}

func saveSettingsHandler(w http.ResponseWriter, r *http.Request, state *AppSignedInState) *AppError {
	c := appengine.NewContext(r)
	account := state.Account

	timezoneName := r.FormValue("timezone_name")
	_, err := time.LoadLocation(timezoneName)
	if err != nil {
		return BadRequest(err, "Malformed timezone_name value")
	}
	account.TimezoneName = timezoneName

	account.DigestEmailAddress = r.FormValue("email_address")
	account.DirectMessagesOnly = r.FormValue("direct_messages_only") == "true"

	err = account.Put(c)
	if err != nil {
		return InternalError(err, "Could not save user")
	}

	state.AddFlash("Settings saved.")
	return RedirectToRoute("settings")
}

func deleteAccountHandler(w http.ResponseWriter, r *http.Request, state *AppSignedInState) *AppError {
	c := appengine.NewContext(r)
	state.Account.Delete(c)
	state.ClearSession()
	return RedirectToRoute("index")
}
