package main

import (
	"github.com/nlopes/slack"
)

type UserLookup struct {
	slackClient *slack.Client
	usersById   map[string]*slack.User
}

func newUserLookup(slackClient *slack.Client) (*UserLookup, error) {
	// Fetch all users in bulk so that we don't need to get them one at a time
	// for each channel message or DM conversation.
	users, err := slackClient.GetUsers()
	if err != nil {
		return nil, err
	}
	usersById := make(map[string]*slack.User)
	for i := range users {
		usersById[users[i].ID] = &users[i]
	}
	return &UserLookup{slackClient, usersById}, nil
}

func (lookup *UserLookup) GetUser(userId string) (*slack.User, error) {
	user, ok := lookup.usersById[userId]
	if !ok {
		user, err := lookup.slackClient.GetUserInfo(userId)
		if err != nil {
			return nil, err
		}
		lookup.usersById[userId] = user
	}
	return user, nil
}
