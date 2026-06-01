package main

import (
	"context"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

type fcmSender struct {
	client *messaging.Client
}

func newFCMSender(ctx context.Context, credFile string) (*fcmSender, error) {
	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsFile(credFile))
	if err != nil {
		return nil, err
	}
	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, err
	}
	return &fcmSender{client: client}, nil
}

// send delivers a DATA-ONLY FCM message shaped the way expo-notifications
// parses it on Android (verified against the installed RemoteNotificationContent
// parser):
//
//	data.title    -> shown notification title
//	data.message  -> shown notification body
//	data.body     -> JSON string, surfaced as content.data in JS (deeplink)
//	data.channelId-> Android notification channel
//
// No top-level Notification field, so expo-notifications fully owns display +
// tap, exactly as it does for Expo-sent pushes.
func (s *fcmSender) send(ctx context.Context, token, title, body, dataJSON string) error {
	msg := &messaging.Message{
		Token: token,
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
		Data: map[string]string{
			"title":     title,
			"message":   body,
			"body":      dataJSON,
			"channelId": "default",
		},
	}
	_, err := s.client.Send(ctx, msg)
	return err
}
