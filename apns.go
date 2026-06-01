package main

import (
	"fmt"
	"os"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/payload"
	"github.com/sideshow/apns2/token"
)

type apnsSender struct {
	client *apns2.Client
	topic  string
}

func newAPNSSender() (*apnsSender, error) {
	authKey, err := token.AuthKeyFromFile(os.Getenv("APNS_KEY_FILE"))
	if err != nil {
		return nil, err
	}
	tok := &token.Token{
		AuthKey: authKey,
		KeyID:   os.Getenv("APNS_KEY_ID"),
		TeamID:  os.Getenv("APNS_TEAM_ID"),
	}

	client := apns2.NewTokenClient(tok)
	if os.Getenv("APNS_PRODUCTION") == "true" {
		client = client.Production()
	} else {
		client = client.Development()
	}

	return &apnsSender{client: client, topic: os.Getenv("APNS_BUNDLE_ID")}, nil
}

// send delivers an APNs alert shaped the way expo-notifications expects on iOS:
//
//	aps.alert.title / aps.alert.body -> shown title/body
//	top-level "body" (JSON string)   -> surfaced as content.data (deeplink)
//
// The iOS path follows Expo's documented direct-APNs format but is unverified
// on a device.
func (s *apnsSender) send(deviceToken, title, body, dataJSON string) error {
	pl := payload.NewPayload().
		AlertTitle(title).
		AlertBody(body).
		Custom("body", dataJSON)

	n := &apns2.Notification{
		DeviceToken: deviceToken,
		Topic:       s.topic,
		Payload:     pl,
	}

	res, err := s.client.Push(n)
	if err != nil {
		return err
	}
	if !res.Sent() {
		return fmt.Errorf("apns rejected: %s", res.Reason)
	}
	return nil
}
