package main

import (
	"errors"
	"time"

	"appengine"
	"appengine/datastore"

	"github.com/nlopes/slack"
)

type Account struct {
	SlackUserId        string         `datastore:",noindex"`
	SlackTeamName      string         `datastore:",noindex"`
	SlackTeamUrl       string         `datastore:",noindex"`
	ApiToken           string         `datastore:",noindex"`
	TimezoneName       string         `datastore:",noindex"`
	TimezoneLocation   *time.Location `datastore:"-,"`
	HasTimezoneSet     bool           `datastore:"-,"`
	DigestEmailAddress string         `datastore:",noindex"`
}

func getAccount(c appengine.Context, slackUserId string) (*Account, error) {
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
	account.HasTimezoneSet = len(account.TimezoneName) > 0
	if !account.HasTimezoneSet {
		account.TimezoneName = "America/Los_Angeles"
	}
	var err error
	account.TimezoneLocation, err = time.LoadLocation(account.TimezoneName)
	if err != nil {
		return err
	}
	return nil
}

func getAllAccounts(c appengine.Context) ([]Account, error) {
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

func (account *Account) Put(c appengine.Context) error {
	key := datastore.NewKey(c, "Account", account.SlackUserId, 0, nil)
	_, err := datastore.Put(c, key, account)
	return err
}

func (account *Account) Delete(c appengine.Context) error {
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
