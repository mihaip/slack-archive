package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
)

type OAuthConfig struct {
	ClientId     string
	ClientSecret string
}

func initSlackOAuthConfig() (config OAuthConfig) {
	configBytes, err := ioutil.ReadFile("config/slack-oauth.json")
	if err != nil {
		log.Panicf("Could not read Slack OAuth config: %s", err.Error())
	}
	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		log.Panicf("Could not parse Slack OAuth config %s: %s", configBytes, err.Error())
	}
	return
}
