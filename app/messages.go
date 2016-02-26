package main

import (
	"bytes"
	"log"
	"strconv"
	"time"

	"github.com/nlopes/slack"
)

const (
	MessageGroupDisplayTimestampFormat = "3:04pm"
)

type Message struct {
	*slack.Message
	timezoneLocation *time.Location
}

func (m *Message) TimestampTime() time.Time {
	floatTimestamp, err := strconv.ParseFloat(m.Timestamp, 64)
	if err != nil {
		log.Println("Could not parse timestamp \"%s\".", m.Timestamp, err)
		return time.Time{}
	}
	return time.Unix(int64(floatTimestamp), 0).In(m.timezoneLocation)
}

type MessageGroup struct {
	Messages []*Message
	Author   *slack.User
}

func safeFormattedDate(date string) string {
	// Insert zero-width spaces every few characters so that Apple Data
	// Detectors and Gmail's calendar event dection don't pick up on these
	// dates.
	var buffer bytes.Buffer
	dateLength := len(date)
	for i := 0; i < dateLength; i += 2 {
		if i == dateLength-1 {
			buffer.WriteString(date[i : i+1])
		} else {
			buffer.WriteString(date[i : i+2])
			if date[i] != ' ' && date[i+1] != ' ' && i < dateLength-2 {
				buffer.WriteString("\u200b")
			}
		}
	}
	return buffer.String()
}

func (mg *MessageGroup) DisplayTimestamp() string {
	return safeFormattedDate(mg.Messages[len(mg.Messages)-1].TimestampTime().Format(MessageGroupDisplayTimestampFormat))
}

func groupMessages(messages []*slack.Message, slackClient *slack.Client, timezoneLocation *time.Location) ([]*MessageGroup, error) {
	var currentGroup *MessageGroup = nil
	groups := make([]*MessageGroup, 0)
	userLookup, err := newUserLookup(slackClient)
	if err != nil {
		return nil, err
	}
	for i := range messages {
		message := &Message{messages[i], timezoneLocation}
		if message.Hidden {
			continue
		}
		var messageAuthor *slack.User = nil
		if message.User != "" {
			messageAuthor, err = userLookup.GetUser(message.User)
			if err != nil {
				return nil, err
			}
		} else if message.BotID != "" {
			messageAuthor, err = userLookup.GetUser(message.BotID)
			if err != nil {
				return nil, err
			}
		} else if message.Username != "" {
			messageAuthor = userLookup.GetUserByName(message.Username)
			if messageAuthor == nil {
				// Synthesize a slack.User from just the given username.
				// It would be nice to also include the profile picture, but the
				// Go library and the Slack API do not agree about how it is
				// represented.
				messageAuthor = newSyntheticUser(message.Username)
			}
		} else {
			log.Printf("Could not determine author for message type %s "+
				"(subtype %s), skipping", message.Type, message.SubType)
			continue
		}
		if currentGroup == nil || messageAuthor.ID != currentGroup.Author.ID {
			currentGroup = &MessageGroup{
				Messages: make([]*Message, 0),
				Author:   messageAuthor,
			}
			groups = append(groups, currentGroup)
		}
		currentGroup.Messages = append(currentGroup.Messages, message)
	}
	return groups, nil
}
