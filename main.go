// sysaru-push is Sysaru's own push relay. It accepts a logical push payload
// from the Sysaru Discourse plugin and delivers it to the device:
//   - Android via FCM (Firebase Admin SDK)
//   - iOS via APNs (token-based auth with a .p8 key)
//
// Both paths emit the message in the shape expo-notifications expects on the
// device, so the app's display/tap/deeplink behaviour is identical to Expo's
// own push service — only the provider changes.
//
// Privacy posture: the relay logs no device tokens and no notification
// content. Credentials are read from files mounted by the runtime (Secret
// Manager on Cloud Run); nothing sensitive is baked into the image.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type sendRequest struct {
	Token    string                 `json:"token"`
	Platform string                 `json:"platform"`
	Title    string                 `json:"title"`
	Body     string                 `json:"body"`
	Data     map[string]interface{} `json:"data"`
}

var (
	bearerToken string
	fcm         *fcmSender
	apns        *apnsSender
)

func main() {
	bearerToken = os.Getenv("RELAY_BEARER_TOKEN")
	ctx := context.Background()

	// FCM (Android) — enabled only when a credentials file is provided.
	if cred := os.Getenv("FIREBASE_CREDENTIALS_FILE"); cred != "" {
		s, err := newFCMSender(ctx, cred)
		if err != nil {
			log.Fatalf("FCM init failed: %v", err)
		}
		fcm = s
		log.Println("FCM (Android) enabled")
	} else {
		log.Println("FCM (Android) disabled — FIREBASE_CREDENTIALS_FILE not set")
	}

	// APNs (iOS) — enabled only when a .p8 key file is provided.
	if key := os.Getenv("APNS_KEY_FILE"); key != "" {
		s, err := newAPNSSender()
		if err != nil {
			log.Fatalf("APNs init failed: %v", err)
		}
		apns = s
		log.Println("APNs (iOS) enabled")
	} else {
		log.Println("APNs (iOS) disabled — APNS_KEY_FILE not set")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/send", handleSend)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("sysaru-push listening on :%s", port)
	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, mux))
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Shared-secret bearer auth (blocks abuse of the relay / FCM quota).
	if bearerToken != "" && r.Header.Get("Authorization") != "Bearer "+bearerToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	// The deeplink data is carried as a JSON string under the "body" key, which
	// expo-notifications parses back into content.data on the device.
	dataJSON := "{}"
	if req.Data != nil {
		if b, err := json.Marshal(req.Data); err == nil {
			dataJSON = string(b)
		}
	}

	var err error
	switch req.Platform {
	case "android":
		if fcm == nil {
			http.Error(w, "FCM not configured", http.StatusServiceUnavailable)
			return
		}
		err = fcm.send(r.Context(), req.Token, req.Title, req.Body, dataJSON)
	case "ios":
		if apns == nil {
			http.Error(w, "APNs not configured", http.StatusServiceUnavailable)
			return
		}
		err = apns.send(req.Token, req.Title, req.Body, dataJSON)
	default:
		http.Error(w, "unknown platform", http.StatusBadRequest)
		return
	}

	if err != nil {
		// Never log the token or notification content — only platform + error.
		log.Printf("send failed (%s): %v", req.Platform, err)
		http.Error(w, "send failed", http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
