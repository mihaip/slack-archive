package main

import (
	"errors"
	"fmt"
	"html"
	"html/template"
	"strings"
	"time"

	"github.com/nlopes/slack"
)

func conversationArchiveUrl(c Conversation) string {
	conversationType, ref := c.ToRef()
	url, _ := RouteUrl("conversation-archive", "type", conversationType, "ref", ref)
	return url
}

type Conversation interface {
	Name() string
	NameHtml() template.HTML
	Purpose() string
	ToRef() (conversationType string, ref string)
	InitFromRef(ref string, slackClient *slack.Client) error
	ArchiveUrl() string
	History(params slack.HistoryParameters, slackClient *slack.Client) (*slack.History, error)
}

type ChannelConversation struct {
	channel *slack.Channel
}

func (c *ChannelConversation) Name() string {
	return fmt.Sprintf("#%s", c.channel.Name)
}

func (c *ChannelConversation) NameHtml() template.HTML {
	return template.HTML(fmt.Sprintf(
		"<span style='%s' class='hash'>#</span>%s",
		Style("conversation.hash"),
		html.EscapeString(c.channel.Name)))
}

func (c *ChannelConversation) Purpose() string {
	return c.channel.Purpose.Value
}

func (c *ChannelConversation) ToRef() (conversationType string, ref string) {
	return "channel", c.channel.ID
}

func (c *ChannelConversation) InitFromRef(ref string, slackClient *slack.Client) error {
	channel, err := slackClient.GetChannelInfo(ref)
	c.channel = channel
	return err
}

func (c *ChannelConversation) ArchiveUrl() string {
	return conversationArchiveUrl(c)
}

func (c *ChannelConversation) History(params slack.HistoryParameters, slackClient *slack.Client) (*slack.History, error) {
	return slackClient.GetChannelHistory(c.channel.ID, params)
}

type PrivateChannelConversation struct {
	group *slack.Group
}

func (c *PrivateChannelConversation) Name() string {
	return fmt.Sprintf("🔒%s", c.group.Name)
}

func (c *PrivateChannelConversation) NameHtml() template.HTML {
	return template.HTML(fmt.Sprintf(
		"<span style='%s' class='lock'>🔒</span>%s",
		Style("conversation.lock"),
		html.EscapeString(c.group.Name)))
}

func (c *PrivateChannelConversation) Purpose() string {
	return c.group.Purpose.Value
}

func (c *PrivateChannelConversation) ToRef() (conversationType string, ref string) {
	return "private-channel", c.group.ID
}

func (c *PrivateChannelConversation) InitFromRef(ref string, slackClient *slack.Client) error {
	group, err := slackClient.GetGroupInfo(ref)
	c.group = group
	return err
}

func (c *PrivateChannelConversation) ArchiveUrl() string {
	return conversationArchiveUrl(c)
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

func (c *DirectMessageConversation) NameHtml() template.HTML {
	imageHtml := fmt.Sprintf(
		"<img src='%s' width='36' height='36' class='user-image' style='%s'>",
		c.user.Profile.Image72,
		Style("conversation.user-image"))
	return template.HTML(fmt.Sprintf(
		"%s%s", imageHtml, html.EscapeString(c.user.Name)))
}

func (c *DirectMessageConversation) Purpose() string {
	return ""
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

func (c *DirectMessageConversation) ArchiveUrl() string {
	return conversationArchiveUrl(c)
}

func (c *DirectMessageConversation) History(params slack.HistoryParameters, slackClient *slack.Client) (*slack.History, error) {
	return slackClient.GetIMHistory(c.im.ID, params)
}

type MultiPartyDirectMessageConversation struct {
	group *slack.Group
	users []*slack.User
}

func (c *MultiPartyDirectMessageConversation) Name() string {
	userNames := make([]string, 0)
	for _, user := range c.users {
		userNames = append(userNames, user.Name)
	}
	return strings.Join(userNames, ", ")
}

func (c *MultiPartyDirectMessageConversation) NameHtml() template.HTML {
	return template.HTML(fmt.Sprintf(
		"<span style='%s' class='count'>%d</span>%s",
		Style("conversation.mpdm-count"),
		len(c.users),
		html.EscapeString(c.Name())))
}

func (c *MultiPartyDirectMessageConversation) Purpose() string {
	return ""
}

func (c *MultiPartyDirectMessageConversation) ToRef() (conversationType string, ref string) {
	return "mpdm-group", c.group.ID
}

func (c *MultiPartyDirectMessageConversation) InitFromRef(ref string, slackClient *slack.Client) error {
	group, err := slackClient.GetGroupInfo(ref)
	if err != nil {
		return err
	}
	c.group = group
	return c.loadUsers(slackClient)
}

func (c *MultiPartyDirectMessageConversation) loadUsers(slackClient *slack.Client) error {
	userLookup, err := newUserLookup(slackClient)
	if err != nil {
		return err
	}
	authTest, err := slackClient.AuthTest()
	if err != nil {
		return err
	}
	users := make([]*slack.User, 0, len(c.group.Members))
	for _, userId := range c.group.Members {
		if userId == authTest.UserID {
			continue
		}
		user, err := userLookup.GetUser(userId)
		if err != nil {
			return err
		}
		users = append(users, user)
	}
	c.users = users
	return nil
}

func (c *MultiPartyDirectMessageConversation) ArchiveUrl() string {
	return conversationArchiveUrl(c)
}

func (c *MultiPartyDirectMessageConversation) History(params slack.HistoryParameters, slackClient *slack.Client) (*slack.History, error) {
	return slackClient.GetGroupHistory(c.group.ID, params)
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
	} else if conversationType == "mpdm-group" {
		conversation = &MultiPartyDirectMessageConversation{}
	} else {
		return nil, errors.New(fmt.Sprintf("Unknown conversation type: %s", conversationType))
	}
	err := conversation.InitFromRef(ref, slackClient)
	return conversation, err
}

func getConversations(slackClient *slack.Client, account *Account) (*Conversations, error) {
	userLookup, err := newUserLookup(slackClient)
	if err != nil {
		return nil, err
	}
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
	conversations.MultiPartyDirectMessages = make([]Conversation, 0)
	for i := range groups {
		group := &groups[i]
		if !group.IsArchived {
			if strings.HasPrefix(group.Name, "mpdm-") {
				// Multi-party direct messages are represented as groups for
				// backwards compatility. Since the Slack Go library doesn't
				// have explicit multi-party direct message support, we use
				// them to synthesize a MultiPartyDirectMessage.
				// (https://api.slack.com/types/group)
				mpdm := &MultiPartyDirectMessageConversation{group: group}
				err := mpdm.loadUsers(slackClient)
				if err != nil {
					return nil, err
				}
				conversations.MultiPartyDirectMessages = append(conversations.MultiPartyDirectMessages, mpdm)
			} else {
				conversations.PrivateChannels = append(conversations.PrivateChannels, &PrivateChannelConversation{group})
			}
		}
	}

	ims, err := slackClient.GetIMChannels()
	if err != nil {
		return nil, err
	}
	conversations.DirectMessages = make([]Conversation, 0, len(ims))
	if len(ims) > 0 {
		for i := range ims {
			im := &ims[i]
			if im.IsUserDeleted {
				continue
			}
			user, err := userLookup.GetUser(im.User)
			if err != nil {
				return nil, err
			}
			conversations.DirectMessages = append(conversations.DirectMessages, &DirectMessageConversation{im, user})
		}
	}

	conversations.AllConversations = make([]Conversation, 0, len(conversations.Channels))
	conversations.AllConversations = append(conversations.AllConversations, conversations.Channels...)
	conversations.AllConversations = append(conversations.AllConversations, conversations.PrivateChannels...)
	conversations.AllConversations = append(conversations.AllConversations, conversations.DirectMessages...)
	conversations.AllConversations = append(conversations.AllConversations, conversations.MultiPartyDirectMessages...)

	return conversations, nil
}

type ConversationArchive struct {
	MessageGroups []*MessageGroup
	StartTime     time.Time
	EndTime       time.Time
}

func (archive *ConversationArchive) Empty() bool {
	for i := range archive.MessageGroups {
		if len(archive.MessageGroups[i].Messages) > 0 {
			return false
		}
	}
	return true
}

func newConversationArchive(conversation Conversation, slackClient *slack.Client, account *Account) (*ConversationArchive, error) {
	messages := make([]*slack.Message, 0)
	now := time.Now().In(account.TimezoneLocation)
	archiveStartTime := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
	archiveEndTime := archiveStartTime.AddDate(0, 0, 1).Add(-time.Second)

	params := slack.HistoryParameters{
		Latest:    fmt.Sprintf("%d", archiveEndTime.Unix()),
		Oldest:    fmt.Sprintf("%d", archiveStartTime.Unix()),
		Count:     1000,
		Inclusive: false,
	}
	for {
		history, err := conversation.History(params, slackClient)
		if err != nil {
			return nil, err
		}
		for i := range history.Messages {
			messages = append([]*slack.Message{&history.Messages[i]}, messages...)
		}
		if !history.HasMore {
			break
		}
		params.Latest = history.Messages[len(history.Messages)-1].Timestamp
	}
	messageGroups, err := groupMessages(messages, slackClient, account.TimezoneLocation)
	if err != nil {
		return nil, err
	}
	return &ConversationArchive{
		MessageGroups: messageGroups,
		StartTime:     archiveStartTime,
		EndTime:       archiveEndTime,
	}, nil
}
