// Copyright (c) 2022 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	waBinary "github.com/lucklrj/whatsmeow/binary"
	waProto "github.com/lucklrj/whatsmeow/binary/proto"
	"github.com/lucklrj/whatsmeow/types"
	"github.com/lucklrj/whatsmeow/types/events"
	"github.com/lucklrj/whatsmeow/util/hkdfutil"
)

func getMediaRetryKey(mediaKey []byte) (cipherKey []byte) {
	return hkdfutil.SHA256(mediaKey, nil, []byte("WhatsApp Media Retry Notification"), 32)
}

func prepareMediaRetryGCM(mediaKey []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(getMediaRetryKey(mediaKey))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCM: %w", err)
	}
	return gcm, nil
}

func encryptMediaRetryReceipt(messageID types.MessageID, mediaKey []byte) (ciphertext, iv []byte, err error) {
	receipt := &waProto.ServerErrorReceipt{
		StanzaId: proto.String(messageID),
	}
	var plaintext []byte
	plaintext, err = proto.Marshal(receipt)
	if err != nil {
		err = fmt.Errorf("failed to marshal payload: %w", err)
		return
	}
	var gcm cipher.AEAD
	gcm, err = prepareMediaRetryGCM(mediaKey)
	if err != nil {
		return
	}
	iv = make([]byte, 12)
	_, err = rand.Read(iv)
	if err != nil {
		panic(err)
	}
	ciphertext = gcm.Seal(plaintext[:0], iv, plaintext, []byte(messageID))
	return
}

// SendMediaRetryReceipt sends a request to the phone to re-upload the media in a message.
//
// The response will come as an *events.MediaRetry. The response will then have to be decrypted
// using DecryptMediaRetryNotification and the same media key passed here.
func (cli *Client) SendMediaRetryReceipt(message *types.MessageInfo, mediaKey []byte) error {
	ciphertext, iv, err := encryptMediaRetryReceipt(message.ID, mediaKey)
	if err != nil {
		return fmt.Errorf("failed to prepare encrypted retry receipt: %w", err)
	}

	rmrAttrs := waBinary.Attrs{
		"jid":     message.Chat,
		"from_me": message.IsFromMe,
	}
	if message.IsGroup {
		rmrAttrs["participant"] = message.Sender
	}

	encryptedRequest := []waBinary.Node{
		{Tag: "enc_p", Content: ciphertext},
		{Tag: "enc_iv", Content: iv},
	}

	err = cli.sendNode(waBinary.Node{
		Tag: "receipt",
		Attrs: waBinary.Attrs{
			"id":   message.ID,
			"to":   cli.Store.ID.ToNonAD(),
			"type": "server-error",
		},
		Content: []waBinary.Node{
			{Tag: "encrypt", Content: encryptedRequest},
			{Tag: "rmr", Attrs: rmrAttrs},
		},
	})
	if err != nil {
		return err
	}
	return nil
}

// DecryptMediaRetryNotification decrypts a media retry notification using the media key.
func DecryptMediaRetryNotification(evt *events.MediaRetry, mediaKey []byte) (*waProto.MediaRetryNotification, error) {
	gcm, err := prepareMediaRetryGCM(mediaKey)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, evt.IV, evt.Ciphertext, []byte(evt.MessageID))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt notification: %w", err)
	}
	var notif waProto.MediaRetryNotification
	err = proto.Unmarshal(plaintext, &notif)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal notification (invalid encryption key?): %w", err)
	}
	return &notif, nil
}

func parseMediaRetryNotification(node *waBinary.Node) (*events.MediaRetry, error) {
	ag := node.AttrGetter()
	var evt events.MediaRetry
	evt.Timestamp = time.Unix(ag.Int64("t"), 0)
	evt.MessageID = types.MessageID(ag.String("id"))
	if !ag.OK() {
		return nil, ag.Error()
	}
	rmr, ok := node.GetOptionalChildByTag("rmr")
	if !ok {
		return nil, &ElementMissingError{Tag: "rmr", In: "retry notification"}
	}
	rmrAG := rmr.AttrGetter()
	evt.ChatID = rmrAG.JID("jid")
	evt.FromMe = rmrAG.Bool("from_me")
	evt.SenderID = rmrAG.OptionalJIDOrEmpty("participant")
	if !rmrAG.OK() {
		return nil, fmt.Errorf("missing attributes in <rmr> tag: %w", rmrAG.Error())
	}

	evt.Ciphertext, ok = node.GetChildByTag("encrypt", "enc_p").Content.([]byte)
	if !ok {
		return nil, &ElementMissingError{Tag: "enc_p", In: fmt.Sprintf("retry notification %s", evt.MessageID)}
	}
	evt.IV, ok = node.GetChildByTag("encrypt", "enc_iv").Content.([]byte)
	if !ok {
		return nil, &ElementMissingError{Tag: "enc_iv", In: fmt.Sprintf("retry notification %s", evt.MessageID)}
	}
	return &evt, nil
}

func (cli *Client) handleMediaRetryNotification(node *waBinary.Node) {
	// TODO handle errors (e.g. <error code="2"/>)
	evt, err := parseMediaRetryNotification(node)
	if err != nil {
		cli.Log.Warnf("Failed to parse media retry notification: %v", err)
		return
	}
	cli.dispatchEvent(evt)
}
