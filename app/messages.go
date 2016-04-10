package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nlopes/slack"
)

const (
	MessageGroupDisplayTimestampFormat = "3:04pm"
	MessageTextBlockquotePrefix1       = "&gt;"
	MessageTextBlockquotePrefix2       = ">>>"
	MessageTextControlRegexp           = "<(.*?)>"
	MessageTextEmojiRegexp             = ":([a-z0-9_\\-+]+):"
)

func textToHtml(text string, truncate bool, slackClient *slack.Client) template.HTML {
	if truncate && len(text) > 700 {
		text = fmt.Sprintf("%s...", text[:700])
	}
	lines := strings.Split(text, "\n")
	if truncate && len(lines) > 5 {
		lines = append(lines[:5], "...")
	}
	htmlPieces := []string{}
	controlRegexp := regexp.MustCompile(MessageTextControlRegexp)
	emojiRegexp := regexp.MustCompile(MessageTextEmojiRegexp)
	for i, line := range lines {
		linePrefix := ""
		lineSuffix := ""
		if strings.HasPrefix(line, MessageTextBlockquotePrefix1) ||
			strings.HasPrefix(line, MessageTextBlockquotePrefix2) {
			line = strings.TrimPrefix(line, MessageTextBlockquotePrefix1)
			line = strings.TrimPrefix(line, MessageTextBlockquotePrefix2)
			if line == "" {
				// Ensure that even empty blockquote lines get rendered.
				line = "\u200b"
			}
			linePrefix = fmt.Sprintf("<blockquote style='%s'>",
				Style("message.blockquote"))
			lineSuffix = "</blockquote>"
		} else {
			if i != 0 {
				lineSuffix = "<br>"
			}
		}
		line = controlRegexp.ReplaceAllStringFunc(line, func(control string) string {
			control = control[1 : len(control)-1]
			anchorText := ""
			pipeIndex := strings.LastIndex(control, "|")
			if pipeIndex != -1 {
				anchorText = control[pipeIndex+1:]
				control = control[:pipeIndex]
			}
			if strings.HasPrefix(control, "@U") {
				userId := strings.TrimPrefix(control, "@")
				userLookup, err := newUserLookup(slackClient)
				if err == nil {
					user, err := userLookup.GetUser(userId)
					if err == nil {
						anchorText = fmt.Sprintf("@%s", user.Name)
						authTest, err := slackClient.AuthTest()
						if err == nil {
							control = fmt.Sprintf("%s/team/%s", authTest.URL, user.Name)
						} else {
							log.Printf("Could get team URL: %s", err)
						}
					} else {
						log.Printf("Could not render user mention: %s", err)
					}
				} else {
					log.Printf("Could not render user mention: %s", err)
				}
			} else if strings.HasPrefix(control, "#C") {
				channelId := strings.TrimPrefix(control, "#")
				channel, err := slackClient.GetChannelInfo(channelId)
				if err == nil {
					anchorText = fmt.Sprintf("#%s", channel.Name)
					authTest, err := slackClient.AuthTest()
					if err == nil {
						control = fmt.Sprintf("%s/team/%s", authTest.URL, channel.Name)
					} else {
						log.Printf("Could get team URL: %s", err)
					}
				} else {
					log.Printf("Could not render channel mention: %s", err)
				}
			} else if strings.HasPrefix(control, "!") {
				command := strings.TrimPrefix(control, "!")
				return fmt.Sprintf("<b>@%s</b>", command)
			}
			if anchorText == "" {
				anchorText = control
			}
			return fmt.Sprintf("<a href='%s' style='%s'>%s</a>",
				control, Style("message.link"), anchorText)
		})
		line = emojiRegexp.ReplaceAllStringFunc(line, func(emojiString string) string {
			shortName := emojiString[1 : len(emojiString)-1]
			if emojiHtml, err := getEmojiHtml(shortName, slackClient); err == nil {
				return fmt.Sprintf("<span title=\"%s\">%s</span>", emojiString, emojiHtml)
			}
			return emojiString
		})

		htmlPieces = append(htmlPieces, linePrefix)
		// Slack's API claims that all HTML is already escaped
		htmlPieces = append(htmlPieces, line)
		htmlPieces = append(htmlPieces, lineSuffix)
	}
	return template.HTML(strings.Join(htmlPieces, ""))
}

type Message struct {
	*slack.Message
	slackClient *slack.Client
	account     *Account
}

func (m *Message) TimestampTime() time.Time {
	floatTimestamp, err := strconv.ParseFloat(m.Timestamp, 64)
	if err != nil {
		log.Println("Could not parse timestamp \"%s\".", m.Timestamp, err)
		return time.Time{}
	}
	return time.Unix(int64(floatTimestamp), 0).In(m.account.TimezoneLocation)
}

func (m *Message) TextHtml() template.HTML {
	return textToHtml(m.Text, false, m.slackClient)
}

func (m *Message) StylePath() string {
	if strings.HasPrefix(m.SubType, "channel_") || strings.HasPrefix(m.SubType, "group_") {
		return "message.automated"
	}
	if m.SubType == "me_message" {
		return "message.me"
	}
	return ""
}

func (m *Message) MessageAttachments() []*MessageAttachment {
	attachments := make([]*MessageAttachment, 0, len(m.Attachments))
	for i := range m.Attachments {
		attachments = append(
			attachments, &MessageAttachment{&m.Attachments[i], m.slackClient})
	}
	return attachments
}

func (m *Message) MessageFile() *MessageFile {
	if m.File == nil {
		return nil
	}
	return &MessageFile{m.File, m.slackClient, m.account}
}

func (m *Message) MessageReactions() []*MessageReaction {
	reactions := make([]*MessageReaction, 0, len(m.Reactions))
	for i := range m.Reactions {
		reactions = append(
			reactions, &MessageReaction{&m.Reactions[i], m.slackClient})
	}
	return reactions
}

type MessageAttachment struct {
	*slack.Attachment
	slackClient *slack.Client
}

func (a *MessageAttachment) TitleHtml() template.HTML {
	return textToHtml(a.Title, false, a.slackClient)
}

func (a *MessageAttachment) TextHtml() template.HTML {
	return textToHtml(a.Text, true, a.slackClient)
}

func (a *MessageAttachment) FieldsHtml() template.HTML {
	htmlPieces := []string{}
	inTable := false
	currentRowCellCount := 0
	for _, field := range a.Fields {
		fieldHtml := fmt.Sprintf(
			"<div style='%s'>%s</div><div>%s</div>",
			Style("message.attachment.field.title"),
			textToHtml(field.Title, false, a.slackClient),
			textToHtml(field.Value, false, a.slackClient))
		if field.Short {
			if !inTable {
				htmlPieces = append(htmlPieces, fmt.Sprintf(
					"<table style='%s'><tr>",
					Style("message.attachment.field.table")))
				inTable = true
				currentRowCellCount = 0
			}
			if currentRowCellCount == 2 {
				htmlPieces = append(htmlPieces, "</tr><tr>")
				currentRowCellCount = 0
			}
			htmlPieces = append(htmlPieces, "<td width='250'>")
			htmlPieces = append(htmlPieces, fieldHtml)
			htmlPieces = append(htmlPieces, "</td>")
			currentRowCellCount += 1
		} else {
			if inTable {
				htmlPieces = append(htmlPieces, "</tr></table>")
				inTable = false
			}
			htmlPieces = append(htmlPieces, fieldHtml)
		}
	}
	if inTable {
		htmlPieces = append(htmlPieces, "</tr></table>")
	}
	return template.HTML(strings.Join(htmlPieces, ""))
}

type MessageFile struct {
	*slack.File
	slackClient *slack.Client
	account     *Account
}

func (f *MessageFile) ThumbnailUrl() (string, error) {
	ref := FileUrlRef{f.ID, f.account.SlackUserId}
	encodedRef, err := ref.Encode()
	if err != nil {
		return "", err
	}
	return AbsoluteRouteUrl("archive-file-thumbnail", "ref", encodedRef)
}

func (f *MessageFile) ThumbnailWidth() int {
	return f.Thumb360W
}

func (f *MessageFile) ThumbnailHeight() int {
	return f.Thumb360H
}

type MessageReaction struct {
	*slack.ItemReaction
	slackClient *slack.Client
}

func (r *MessageReaction) Emoji() template.HTML {
	if emojiHtml, err := getEmojiHtml(r.Name, r.slackClient); err == nil {
		return template.HTML(emojiHtml)
	}
	return template.HTML(r.Name)
}

func (r *MessageReaction) Summary() (string, error) {
	userLookup, err := newUserLookup(r.slackClient)
	if err != nil {
		return "", err
	}
	names := make([]string, len(r.Users))
	for i := range r.Users {
		if user, err := userLookup.GetUser(r.Users[i]); err == nil {
			names[i] = user.Name
		} else {
			names[i] = r.Users[i]
		}
	}
	return fmt.Sprintf("%s reacted with :%s:",
		strings.Join(names, ", "), r.Name), nil
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

func (mg *MessageGroup) shouldContainMessage(message *Message, messageAuthor *slack.User) bool {
	if messageAuthor.ID != mg.Author.ID {
		return false
	}
	lastMessage := mg.Messages[len(mg.Messages)-1]
	timestampDelta := message.TimestampTime().Sub(lastMessage.TimestampTime())
	if timestampDelta > time.Minute*10 {
		return false
	}
	return true
}

func (mg *MessageGroup) FromBot() bool {
	return mg.Messages[0].SubType == "bot_message"
}

func (mg *MessageGroup) DisplayTimestamp() string {
	return safeFormattedDate(mg.Messages[0].TimestampTime().Format(
		MessageGroupDisplayTimestampFormat))
}

func groupMessages(messages []*slack.Message, slackClient *slack.Client, account *Account) ([]*MessageGroup, error) {
	var currentGroup *MessageGroup = nil
	groups := make([]*MessageGroup, 0)
	userLookup, err := newUserLookup(slackClient)
	if err != nil {
		return nil, err
	}
	for i := range messages {
		message := &Message{messages[i], slackClient, account}
		if message.Hidden {
			continue
		}
		messageAuthor, _ := userLookup.GetUserForMessage(messages[i])
		if messageAuthor == nil {
			log.Printf("Could not determine author for message type %s "+
				"(subtype %s), skipping", message.Type, message.SubType)
			continue
		}
		if currentGroup == nil || !currentGroup.shouldContainMessage(message, messageAuthor) {
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
