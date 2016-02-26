package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"

	"appengine"
	"appengine/datastore"
	"appengine/mail"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/nlopes/slack"
)

var router *mux.Router
var slackOAuthConfig OAuthConfig
var sessionStore *sessions.CookieStore
var sessionConfig SessionConfig
var templates map[string]*Template

func init() {
	templates = loadTemplates()
	sessionStore, sessionConfig = initSession()
	slackOAuthConfig = initSlackOAuthConfig()

	router = mux.NewRouter()
	router.Handle("/", AppHandler(indexHandler)).Name("index")

	router.Handle("/session/sign-in", AppHandler(signInHandler)).Name("sign-in").Methods("POST")
	router.Handle("/session/sign-out", AppHandler(signOutHandler)).Name("sign-out").Methods("POST")
	router.Handle("/slack/callback", AppHandler(slackOAuthCallbackHandler)).Name("slack-callback")

	router.Handle("/archive/{type}/{ref}", SignedInAppHandler(conversationArchiveHandler)).Name("conversation-archive")
	router.Handle("/send-conversation", SignedInAppHandler(sendConversationArchiveHandler)).Name("send-conversation-archive").Methods("POST")

	http.Handle("/", router)
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

	slackClient := slack.New(account.ApiToken)

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
		"Frequency":    account.Frequency,
		"EmailAddress": emailAddress,
	}
	var data = map[string]interface{}{
		"User":            user,
		"Team":            team,
		"Conversations":   conversations,
		"SettingsSummary": settingsSummary,
		"DetectTimezone":  !account.HasTimezoneSet,
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
	authCodeUrlQuery.Set("scope",
		// Basic user info
		"users:read "+
			// Team info
			"team:read "+
			// Channel archive
			"channels:read channels:history "+
			// Private channel archive
			"groups:read groups:history "+
			// Direct message archive
			"im:read im:history "+
			// Multi-party direct mesage archive
			"mpim:read mpim:history")
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
	code := r.FormValue("code")
	redirectUrl := AbsolutePathUrl(r.URL.Path)
	token, _, err := slack.GetOAuthToken(
		slackOAuthConfig.ClientId, slackOAuthConfig.ClientSecret, code,
		redirectUrl, false)
	if err != nil {
		return InternalError(err, "Could not exchange OAuth code")
	}

	slackClient := slack.New(token)
	authTest, err := slackClient.AuthTest()
	if err != nil {
		return SlackFetchError(err, "user")
	}

	c := appengine.NewContext(r)
	account, err := getAccount(c, authTest.UserID)
	if err != nil && err != datastore.ErrNoSuchEntity {
		return InternalError(err, "Could not look up user")
	}
	if account == nil {
		account = &Account{
			SlackUserId:   authTest.UserID,
			SlackTeamName: authTest.Team,
			SlackTeamUrl:  authTest.URL,
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

	archive, err := newConversationArchive(conversation, state.SlackClient, state.Account)
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
		return InternalError(err, "Could not send digest")
	}
	if sent {
		state.AddFlash("Emailed archive!")
	} else {
		state.AddFlash("No archive was sent, it was empty or disabled.")
	}
	return RedirectToRoute("conversation-archive", "type", conversationType, "ref", ref)
}

func sendConversationArchive(conversation Conversation, account *Account, c appengine.Context) (bool, error) {
	slackClient := slack.New(account.ApiToken)
	emailAddress, err := account.GetDigestEmailAddress(slackClient)
	if err != nil {
		return false, err
	}
	if emailAddress == "disabled" {
		return false, nil
	}
	archive, err := newConversationArchive(conversation, slackClient, account)
	if err != nil {
		return false, err
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
