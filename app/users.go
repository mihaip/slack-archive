package main

import (
	"fmt"
	"strings"

	"github.com/nlopes/slack"
)

const (
	SyntheticUserImageUrlTemplate = "https://i1.wp.com/slack.global.ssl.fastly.net/66f9/img/avatars/ava_0025-%d.png?ssl=1"
	SyntheticBotUserImageUrl = "https://slack.global.ssl.fastly.net/66f9/img/default_application_icon.png"
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

func (lookup *UserLookup) GetUserForMessage(message *slack.Message) (*slack.User, error) {
	var err error
	if message.User != "" {
		messageAuthor, err := lookup.GetUser(message.User)
		if err == nil {
			return messageAuthor, nil
		}
	}
	if message.BotID != "" {
		messageAuthor, err := lookup.GetUser(message.BotID)
		if err == nil {
			return messageAuthor, nil
		}
	}
	if message.Username != "" {
		messageAuthor := lookup.GetUserByName(message.Username)
		if messageAuthor == nil {
			// Synthesize a slack.User from just the given username.
			// It would be nice to also include the profile picture, but the
			// Go library and the Slack API do not agree about how it is
			// represented.
			return newSyntheticUser(message.Username), nil
		}
	}
	// Fall back on a synthetic user with the ID, it's better than nothing.
	if message.User != "" {
		return newSyntheticUser(message.User), nil
	}
	if message.BotID != "" {
		return newSyntheticBotUser(message.BotID), nil
	}

	return nil, err
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

func newSyntheticBotUser(name string) *slack.User {
	return &slack.User{
		ID:   fmt.Sprintf("synthetic-%s", name),
		Name: name,
		Profile: slack.UserProfile{
			Image24:  SyntheticBotUserImageUrl,
			Image32:  SyntheticBotUserImageUrl,
			Image48:  SyntheticBotUserImageUrl,
			Image72:  SyntheticBotUserImageUrl,
			Image192: SyntheticBotUserImageUrl,
		},
	}
}
