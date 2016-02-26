package main

import (
	"errors"
	"fmt"

	"github.com/nlopes/slack"
)

func conversationHistoryUrl(c Conversation) string {
	conversationType, ref := c.ToRef()
	url, _ := RouteUrl("history", "type", conversationType, "ref", ref)
	return url
}

type Conversation interface {
	Name() string
	ToRef() (conversationType string, ref string)
	InitFromRef(ref string, slackClient *slack.Client) error
	HistoryUrl() string
	History(params slack.HistoryParameters, slackClient *slack.Client) (*slack.History, error)
}

type ChannelConversation struct {
	channel *slack.Channel
}

func (c *ChannelConversation) Name() string {
	return c.channel.Name
}

func (c *ChannelConversation) ToRef() (conversationType string, ref string) {
	return "channel", c.channel.ID
}

func (c *ChannelConversation) InitFromRef(ref string, slackClient *slack.Client) error {
	channel, err := slackClient.GetChannelInfo(ref)
	c.channel = channel
	return err
}

func (c *ChannelConversation) HistoryUrl() string {
	return conversationHistoryUrl(c)
}

func (c *ChannelConversation) History(params slack.HistoryParameters, slackClient *slack.Client) (*slack.History, error) {
	return slackClient.GetChannelHistory(c.channel.ID, params)
}

type PrivateChannelConversation struct {
	group *slack.Group
}

func (c *PrivateChannelConversation) Name() string {
	return c.group.Name
}

func (c *PrivateChannelConversation) ToRef() (conversationType string, ref string) {
	return "private-channel", c.group.ID
}

func (c *PrivateChannelConversation) InitFromRef(ref string, slackClient *slack.Client) error {
	group, err := slackClient.GetGroupInfo(ref)
	c.group = group
	return err
}

func (c *PrivateChannelConversation) HistoryUrl() string {
	return conversationHistoryUrl(c)
}

func (c *PrivateChannelConversation) History(params slack.HistoryParameters, slackClient *slack.Client) (*slack.History, error) {
	return slackClient.GetGroupHistory(c.group.ID, params)
}

type DirectMessageConversation struct {
	im   *slack.IM
	user *slack.User
}

func (c *DirectMessageConversation) Name() string {
	return c.user.Name
}

func (c *DirectMessageConversation) ToRef() (conversationType string, ref string) {
	return "dm", c.im.ID
}

func (c *DirectMessageConversation) InitFromRef(ref string, slackClient *slack.Client) error {
	ims, err := slackClient.GetIMChannels()
	if err != nil {
		return err
	}
	c.im = nil
	for i := range ims {
		if ims[i].ID == ref {
			c.im = &ims[i]
			break
		}
	}
	if c.im == nil {
		return errors.New(fmt.Sprintf("Could not find direct message with ID %s", ref))
	}
	user, err := slackClient.GetUserInfo(c.im.User)
	if err != nil {
		return err
	}
	c.user = user
	return nil
}

func (c *DirectMessageConversation) HistoryUrl() string {
	return conversationHistoryUrl(c)
}

func (c *DirectMessageConversation) History(params slack.HistoryParameters, slackClient *slack.Client) (*slack.History, error) {
	return slackClient.GetIMHistory(c.im.ID, params)
}

type Conversations struct {
	AllConversations         []Conversation
	Channels                 []Conversation
	PrivateChannels          []Conversation
	DirectMessages           []Conversation
	MultiPartyDirectMessages []Conversation
}

func getConversationFromRef(conversationType string, ref string, slackClient *slack.Client) (Conversation, error) {
	var conversation Conversation
	if conversationType == "channel" {
		conversation = &ChannelConversation{}
	} else if conversationType == "private-channel" {
		conversation = &PrivateChannelConversation{}
	} else if conversationType == "dm" {
		conversation = &DirectMessageConversation{}
	} else {
		return nil, errors.New(fmt.Sprintf("Unknown conversation type: %s", conversationType))
	}
	err := conversation.InitFromRef(ref, slackClient)
	return conversation, err
}

func getConversations(slackClient *slack.Client, account *Account) (*Conversations, error) {
	conversations := &Conversations{}

	channels, err := slackClient.GetChannels(false)
	if err != nil {
		return nil, err
	}
	conversations.Channels = make([]Conversation, 0, len(channels))
	for i := range channels {
		channel := &channels[i]
		if channel.IsMember && !channel.IsArchived {
			conversations.Channels = append(conversations.Channels, &ChannelConversation{channel})
		}
	}

	groups, err := slackClient.GetGroups(false)
	if err != nil {
		return nil, err
	}
	conversations.PrivateChannels = make([]Conversation, 0, len(groups))
	for i := range groups {
		group := &groups[i]
		if !group.IsArchived {
			conversations.PrivateChannels = append(conversations.PrivateChannels, &PrivateChannelConversation{group})
		}
	}

	ims, err := slackClient.GetIMChannels()
	if err != nil {
		return nil, err
	}
	conversations.DirectMessages = make([]Conversation, 0, len(ims))
	if len(ims) > 0 {
		userLookup, err := newUserLookup(slackClient)
		if err != nil {
			return nil, err
		}
		for i := range ims {
			user, err := userLookup.GetUser(ims[i].User)
			if err != nil {
				return nil, err
			}
			conversations.DirectMessages = append(conversations.DirectMessages, &DirectMessageConversation{&ims[i], user})
		}
	}

	// TODO: add multi-party direct message support to the Slack Go library

	conversations.AllConversations = make([]Conversation, 0, len(conversations.Channels))
	conversations.AllConversations = append(conversations.AllConversations, conversations.Channels...)
	conversations.AllConversations = append(conversations.AllConversations, conversations.PrivateChannels...)
	conversations.AllConversations = append(conversations.AllConversations, conversations.DirectMessages...)
	conversations.AllConversations = append(conversations.AllConversations, conversations.MultiPartyDirectMessages...)

	return conversations, nil
}
