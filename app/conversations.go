package main

import (
	"github.com/nlopes/slack"
)

type ConversationType int

const (
	ConversationTypeChannel ConversationType = iota
	ConversationTypePrivateChannel
	ConversationTypeDirectMessage
	ConversationTypeMultiPartyDirectMessage
)

type Conversation struct {
	Id   string
	Name string
	Type ConversationType
}

type Conversations struct {
	AllConversations         []*Conversation
	Channels                 []*Conversation
	PrivateChannels          []*Conversation
	DirectMessages           []*Conversation
	MultiPartyDirectMessages []*Conversation
}

func newChannelConversation(channel *slack.Channel, account *Account) *Conversation {
	return newConversation(ConversationTypeChannel, channel.ID, channel.Name, account)
}

func newPrivateChannelConversation(group *slack.Group, account *Account) *Conversation {
	return newConversation(ConversationTypePrivateChannel, group.ID, group.Name, account)
}

func newDirectMessageConversation(im *slack.IM, user *slack.User, account *Account) *Conversation {
	return newConversation(ConversationTypeDirectMessage, im.ID, user.Name, account)
}

func newConversation(conversationType ConversationType, id string, name string, account *Account) *Conversation {
	return &Conversation{
		Id:   id,
		Name: name,
		Type: conversationType,
	}
}

func getConversations(slackClient *slack.Client, account *Account) (*Conversations, error) {
	conversations := &Conversations{}

	channels, err := slackClient.GetChannels(false)
	if err != nil {
		return nil, err
	}
	conversations.Channels = make([]*Conversation, 0, len(channels))
	for i := range channels {
		conversations.Channels = append(conversations.Channels, newChannelConversation(&channels[i], account))
	}

	groups, err := slackClient.GetGroups(false)
	if err != nil {
		return nil, err
	}
	conversations.PrivateChannels = make([]*Conversation, 0, len(groups))
	for i := range groups {
		conversations.PrivateChannels = append(conversations.PrivateChannels, newPrivateChannelConversation(&groups[i], account))
	}

	ims, err := slackClient.GetIMChannels()
	if err != nil {
		return nil, err
	}
	conversations.DirectMessages = make([]*Conversation, 0, len(ims))
	if len(ims) > 0 {
		// DMs only include user IDs, fetch all users in bulk so that we don't
		// need to get them one at a time for each DM conversation.
		users, err := slackClient.GetUsers()
		if err != nil {
			return nil, err
		}
		usersById := make(map[string]*slack.User)
		for i := range users {
			usersById[users[i].ID] = &users[i]
		}
		for i := range ims {
			user, ok := usersById[ims[i].User]
			if !ok {
				user, err = slackClient.GetUserInfo(ims[i].User)
				if err != nil {
					return nil, err
				}
			}
			conversations.DirectMessages = append(conversations.DirectMessages, newDirectMessageConversation(&ims[i], user, account))
		}
	}

	// TODO: add multi-party direct message support to the Slack Go library

	conversations.AllConversations = make([]*Conversation, 0, len(conversations.Channels))
	conversations.AllConversations = append(conversations.AllConversations, conversations.Channels...)
	conversations.AllConversations = append(conversations.AllConversations, conversations.PrivateChannels...)
	conversations.AllConversations = append(conversations.AllConversations, conversations.DirectMessages...)
	conversations.AllConversations = append(conversations.AllConversations, conversations.MultiPartyDirectMessages...)

	return conversations, nil
}
