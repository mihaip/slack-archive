package main

import (
	"errors"
	"fmt"
	"html"
	"html/template"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

const (
	ConversationArchiveDateFormat = "January 2, 2006"
)

func conversationArchiveUrl(c Conversation) string {
	conversationType, ref := c.ToRef()
	url, _ := RouteUrl("conversation-archive", "type", conversationType, "ref", ref)
	return url
}

type Conversation interface {
	Id() string
	Name() string
	NameHtml() template.HTML
	Purpose() string
	ToRef() (conversationType string, ref string)
	InitFromRef(ref string, slackClient *slack.Client) error
	ArchiveUrl() string
}

type ChannelConversation struct {
	channel *slack.Channel
}

func (c *ChannelConversation) Id() string {
	return c.channel.ID
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
	channel, err := slackClient.GetConversationInfo(ref, false)
	c.channel = channel
	return err
}

func (c *ChannelConversation) ArchiveUrl() string {
	return conversationArchiveUrl(c)
}

type PrivateChannelConversation struct {
	channel *slack.Channel
}

func (c *PrivateChannelConversation) Id() string {
	return c.channel.ID
}

func (c *PrivateChannelConversation) Name() string {
	return fmt.Sprintf("🔒%s", c.channel.Name)
}

func (c *PrivateChannelConversation) NameHtml() template.HTML {
	return template.HTML(fmt.Sprintf(
		"<span style='%s' class='lock'>🔒</span>%s",
		Style("conversation.lock"),
		html.EscapeString(c.channel.Name)))
}

func (c *PrivateChannelConversation) Purpose() string {
	return c.channel.Purpose.Value
}

func (c *PrivateChannelConversation) ToRef() (conversationType string, ref string) {
	return "private-channel", c.channel.ID
}

func (c *PrivateChannelConversation) InitFromRef(ref string, slackClient *slack.Client) error {
	channel, err := slackClient.GetConversationInfo(ref, false)
	c.channel = channel
	return err
}

func (c *PrivateChannelConversation) ArchiveUrl() string {
	return conversationArchiveUrl(c)
}

type DirectMessageConversation struct {
	im   *slack.Channel
	user *slack.User
}

func (c *DirectMessageConversation) Id() string {
	return c.im.ID
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
	im, err := slackClient.GetConversationInfo(ref, false)
	if err != nil {
		return err
	}
	c.im = im
	user, err := slackClient.GetUserInfo(im.User)
	if err != nil {
		return fmt.Errorf("Could not look up user %s for DM %s: %s", c.im.User, ref, err.Error())
	}
	c.user = user
	return nil
}

func (c *DirectMessageConversation) ArchiveUrl() string {
	return conversationArchiveUrl(c)
}

type MultiPartyDirectMessageConversation struct {
	mpim  *slack.Channel
	users []*slack.User
}

func (c *MultiPartyDirectMessageConversation) Id() string {
	return c.mpim.ID
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
	return "mpdm-group", c.mpim.ID
}

func (c *MultiPartyDirectMessageConversation) InitFromRef(ref string, slackClient *slack.Client) error {
	mpim, err := slackClient.GetConversationInfo(ref, false)
	if err != nil {
		return err
	}
	c.mpim = mpim
	return c.loadUsers(slackClient)
}

func (c *MultiPartyDirectMessageConversation) loadUsers(slackClient *slack.Client) error {
	userLookup, err := newUserLookup(slackClient)
	if err != nil {
		return err
	}
	return c.loadUsersWithLookup(slackClient, userLookup)
}

func (c *MultiPartyDirectMessageConversation) loadUsersWithLookup(slackClient *slack.Client, userLookup *UserLookup) error {
	authTest, err := slackClient.AuthTest()
	if err != nil {
		return err
	}
	members, _, err := slackClient.GetUsersInConversation(&slack.GetUsersInConversationParameters{
		ChannelID: c.mpim.ID,
		Limit:     100,
	})
	if err != nil {
		return err
	}
	users := make([]*slack.User, 0, len(members))
	for _, userId := range members {
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

	params := slack.GetConversationsForUserParameters{
		Limit: 1000,
		Types: []string{"public_channel", "private_channel", "mpim", "im"},
	}
	slackConversations, _, err := slackClient.GetConversationsForUser(&params)
	if err != nil {
		return nil, err
	}
	conversations.Channels = make([]Conversation, 0)
	conversations.PrivateChannels = make([]Conversation, 0)
	conversations.MultiPartyDirectMessages = make([]Conversation, 0)
	conversations.DirectMessages = make([]Conversation, 0)
	conversations.AllConversations = make([]Conversation, 0, len(slackConversations))
	for i := range slackConversations {
		slackConversation := &slackConversations[i]
		var conversation Conversation = nil
		if slackConversation.IsChannel {
			if !slackConversation.IsArchived {
				conversation = &ChannelConversation{channel: slackConversation}
				conversations.Channels = append(conversations.Channels, conversation)
			}
		} else if slackConversation.IsGroup && !slackConversation.IsMpIM {
			if !slackConversation.IsArchived {
				conversation = &PrivateChannelConversation{channel: slackConversation}
				conversations.PrivateChannels = append(conversations.PrivateChannels, conversation)
			}
		} else if slackConversation.IsMpIM {
			mpdm := &MultiPartyDirectMessageConversation{mpim: slackConversation}
			err := mpdm.loadUsersWithLookup(slackClient, userLookup)
			if err != nil {
				return nil, err
			}
			conversation = mpdm
			conversations.MultiPartyDirectMessages = append(conversations.MultiPartyDirectMessages, mpdm)
		} else if slackConversation.IsIM {
			user, err := userLookup.GetUser(slackConversation.User)
			if err != nil {
				return nil, err
			}
			conversation = &DirectMessageConversation{im: slackConversation, user: user}
			conversations.DirectMessages = append(conversations.DirectMessages, conversation)
		} else {
			return nil, fmt.Errorf("Unknown Slack conversation: %s", slackConversation.ID)
		}
		if conversation != nil {
			conversations.AllConversations = append(conversations.AllConversations, conversation)
		}
	}

	return conversations, nil
}

type ConversationArchive struct {
	Conversation  Conversation
	MessageGroups []*MessageGroup
	MessageCount  int
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

func (archive *ConversationArchive) DisplayDate() string {
	return safeFormattedDate(archive.EndTime.Format(ConversationArchiveDateFormat))
}

func newConversationArchive(conversation Conversation, slackClient *slack.Client, account *Account, devMode bool) (*ConversationArchive, error) {
	messages := make([]*slack.Message, 0)
	now := time.Now().In(account.TimezoneLocation)
	var archiveStartTime time.Time
	var archiveEndTime time.Time
	if !devMode {
		archiveStartTime = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
		archiveEndTime = archiveStartTime.AddDate(0, 0, 1).Add(-time.Second)
	} else {
		archiveStartTime = now.AddDate(0, 0, -1)
		archiveEndTime = now
	}

	params := slack.GetConversationHistoryParameters{
		ChannelID: conversation.Id(),
		Latest:    fmt.Sprintf("%d", archiveEndTime.Unix()),
		Oldest:    fmt.Sprintf("%d", archiveStartTime.Unix()),
		Limit:     1000,
		Inclusive: false,
	}
	for {
		history, err := slackClient.GetConversationHistory(&params)
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
	messageGroups, err := groupMessages(messages, slackClient, account)
	if err != nil {
		return nil, err
	}
	return &ConversationArchive{
		Conversation:  conversation,
		MessageGroups: messageGroups,
		MessageCount:  len(messages),
		StartTime:     archiveStartTime,
		EndTime:       archiveEndTime,
	}, nil
}
