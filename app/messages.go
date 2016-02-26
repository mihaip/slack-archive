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
