package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
)

type FileUrlRef struct {
	FileId      string
	SlackUserId string
}

type FilesConfig struct {
	EncryptionKey string
}

func loadFileUrlRefEncryptionKey() []byte {
	configBytes, err := ioutil.ReadFile("config/files.json")
	if err != nil {
		log.Panicf("Could not read files config: %s", err.Error())
	}
	var filesConfig FilesConfig
	err = json.Unmarshal(configBytes, &filesConfig)
	if err != nil {
		log.Panicf("Could not parse files config %s: %s", configBytes, err.Error())
	}

	encryptionKey, err := base64.StdEncoding.DecodeString(sessionConfig.EncryptionKey)
	if err != nil {
		log.Panicf("Could not decode file config encryption key %s: %s", sessionConfig.EncryptionKey, err.Error())
	}
	return encryptionKey
}

func (f *FileUrlRef) Encode() (string, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(fileUrlRefEncryptionKey)
	if err != nil {
		return "", err
	}
	ciphertext := make([]byte, aes.BlockSize+len(b))
	// A random initialization vector with the length of the block size is
	// prepended to the resulting ciphertext.
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	cfb := cipher.NewCFBEncrypter(block, iv)
	cfb.XORKeyStream(ciphertext[aes.BlockSize:], []byte(b))
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

func DecodeFileUrlRef(encoded string) (*FileUrlRef, error) {
	ciphertext, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(fileUrlRefEncryptionKey)
	size := block.BlockSize()
	if len(ciphertext) <= block.BlockSize() {
		return nil, errors.New("malformed encrypted FileUrlRef")
	}
	// Extract the initialization vector.
	iv := ciphertext[:size]
	b := ciphertext[size:]
	cfb := cipher.NewCFBDecrypter(block, iv)
	cfb.XORKeyStream(b, b)
	var f FileUrlRef
	err = json.Unmarshal(b, &f)
	if err != nil {
		return nil, err
	}
	return &f, nil
}
