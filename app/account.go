package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/urlfetch"

	"github.com/slack-go/slack"
)

type Account struct {
	SlackUserId        string         `datastore:",noindex"`
	SlackTeamName      string         `datastore:",noindex"`
	SlackTeamUrl       string         `datastore:",noindex"`
	ApiToken           string         `datastore:",noindex"`
	TimezoneName       string         `datastore:",noindex"`
	TimezoneLocation   *time.Location `datastore:"-,"`
	DigestEmailAddress string         `datastore:",noindex"`
	DirectMessagesOnly bool           `datastore:",noindex"`
}

func getAccount(c context.Context, slackUserId string) (*Account, error) {
	key := datastore.NewKey(c, "Account", slackUserId, 0, nil)
	account := new(Account)
	err := datastore.Get(c, key, account)
	if err != nil {
		return nil, err
	}

	err = initAccount(account)
	if err != nil {
		return nil, err
	}
	return account, nil
}

func initAccount(account *Account) error {
	if len(account.TimezoneName) == 0 {
		account.TimezoneName = "America/Los_Angeles"
	}
	var err error
	account.TimezoneLocation, err = time.LoadLocation(account.TimezoneName)
	if err != nil {
		return err
	}
	return nil
}

func getAllAccounts(c context.Context) ([]Account, error) {
	q := datastore.NewQuery("Account")
	var accounts []Account
	_, err := q.GetAll(c, &accounts)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		err = initAccount(&accounts[i])
		if err != nil {
			return nil, err
		}
	}
	return accounts, nil
}

func (account *Account) Put(c context.Context) error {
	key := datastore.NewKey(c, "Account", account.SlackUserId, 0, nil)
	_, err := datastore.Put(c, key, account)
	return err
}

func (account *Account) Delete(c context.Context) error {
	key := datastore.NewKey(c, "Account", account.SlackUserId, 0, nil)
	err := datastore.Delete(c, key)
	return err
}

func (account *Account) GetDigestEmailAddress(slackClient *slack.Client) (string, error) {
	if len(account.DigestEmailAddress) > 0 {
		return account.DigestEmailAddress, nil
	}
	user, err := slackClient.GetUserInfo(account.SlackUserId)
	if err != nil {
		return "", err
	}
	if len(user.Profile.Email) > 0 {
		return user.Profile.Email, nil
	}
	return "", errors.New("No email addresses found in Slack profile")
}

func (account *Account) NewSlackClient(c context.Context) *slack.Client {
	// The Slack API uses the default HTTP transport, so we need to override
	// it to get it to work on App Engine. This is normally done for all
	// handlers, but since we're in a delay function that code has not run.
	c, _ = context.WithTimeout(c, time.Second*60)
	appengineTransport := &urlfetch.Transport{Context: c}
	http.DefaultTransport = &CachingTransport{
		Transport: appengineTransport,
		Context:   c,
	}
	return slack.New(account.ApiToken)
}
