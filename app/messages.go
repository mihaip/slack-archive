package main

import (
	"log"
	"strconv"
	"time"

	"github.com/nlopes/slack"
)

type Message struct {
	*slack.Message
}

func (m *Message) TimestampTime() time.Time {
	floatTimestamp, err := strconv.ParseFloat(m.Timestamp, 64)
	if err != nil {
		log.Println("Could not parse timestamp \"%s\".", m.Timestamp, err)
		return time.Time{}
	}
	return time.Unix(int64(floatTimestamp), 0)
}

type MessageGroup struct {
	Messages []*Message
	Author   *slack.User
}

func groupMessages(messages []*slack.Message, slackClient *slack.Client) ([]*MessageGroup, error) {
	var currentGroup *MessageGroup = nil
	groups := make([]*MessageGroup, 0)
	userLookup, err := newUserLookup(slackClient)
	if err != nil {
		return nil, err
	}
	for i := range messages {
		message := &Message{messages[i]}
		if currentGroup == nil || message.User != currentGroup.Author.ID {
			author, err := userLookup.GetUser(message.User)
			if err != nil {
				return nil, err
			}
			currentGroup = &MessageGroup{
				Messages: make([]*Message, 0),
				Author:   author,
			}
			groups = append(groups, currentGroup)
		}
		currentGroup.Messages = append(currentGroup.Messages, message)
	}
	return groups, nil
}
