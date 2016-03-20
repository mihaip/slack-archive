package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
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
