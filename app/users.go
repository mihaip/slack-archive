package main

import (
	"fmt"
	"strings"

	"github.com/nlopes/slack"
)

const (
	SyntheticUserImageUrlTemplate = "https://i1.wp.com/slack.global.ssl.fastly.net/66f9/img/avatars/ava_0025-%d.png?ssl=1"
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

func (lookup *UserLookup) GetUserByName(name string) *slack.User {
	for _, user := range lookup.usersById {
		// Use a case-insensitive comparison, we get names with different
		// capitalization in bot messages vs. user profiles.
		if strings.EqualFold(name, user.Name) {
			return user
		}
	}
	return nil
}

func newSyntheticUser(name string) *slack.User {
	return &slack.User{
		ID:   fmt.Sprintf("synthetic-%s", name),
		Name: name,
		Profile: slack.UserProfile{
			Image24:  fmt.Sprintf(SyntheticUserImageUrlTemplate, 24),
			Image32:  fmt.Sprintf(SyntheticUserImageUrlTemplate, 32),
			Image48:  fmt.Sprintf(SyntheticUserImageUrlTemplate, 48),
			Image72:  fmt.Sprintf(SyntheticUserImageUrlTemplate, 72),
			Image192: fmt.Sprintf(SyntheticUserImageUrlTemplate, 192),
		},
	}
}
