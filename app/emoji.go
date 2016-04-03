package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/nlopes/slack"
)

type Emoji struct {
	Name                string   `json:"name"`
	UnicodeCodePointHex string   `json:"unified"`
	CommonShortName     string   `json:"short_name"`
	AllShortNames       []string `json:"short_names"`
}

func loadEmoji() map[string]*Emoji {
	// Emoji data from https://github.com/iamcal/emoji-data/
	configBytes, err := ioutil.ReadFile("config/emoji.json")
	if err != nil {
		log.Panicf("Could not read emoji config: %s", err.Error())
	}
	var emojis []Emoji
	err = json.Unmarshal(configBytes, &emojis)
	if err != nil {
		log.Panicf("Could not parse emoji config %s: %s", configBytes, err.Error())
	}
	result := make(map[string]*Emoji, len(emojis))
	for i := range emojis {
		emoji := &emojis[i]
		for _, shortName := range emoji.AllShortNames {
			result[shortName] = emoji
		}
	}
	return result
}

func getEmojiHtml(shortName string, slackClient *slack.Client) (string, error) {
	if emoji, ok := emojiByShortName[shortName]; ok {
		return fmt.Sprintf("&#x%s;", emoji.UnicodeCodePointHex), nil
	}
	customEmojiMap, err := slackClient.GetEmoji()
	if err != nil {
		return "", err
	}
	if emojiImageUrl, ok := customEmojiMap[shortName]; ok {
		return fmt.Sprintf("<img src='%s' alt='' width='20' height='20' "+
			"style='vertical-align:text-bottom'>", emojiImageUrl), nil
	}
	return "", errors.New(fmt.Sprintf("Emoji '%s' not found", shortName))
}
