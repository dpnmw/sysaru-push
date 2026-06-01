# sysaru-push

Sysaru's own push relay. It receives a logical push payload from the Sysaru
Discourse plugin and delivers it to the device, replacing Expo's push service
(`exp.host`) with infrastructure you control:

- **Android** → FCM (Firebase Admin SDK)
- **iOS** → APNs (token-based auth with a `.p8` key)

Both paths emit the message in the shape `expo-notifications` parses on the
device, so the app's display / tap / deeplink behaviour is identical to Expo's
own push service — only the provider changes. The app keeps using
`expo-notifications` and `getDevicePushTokenAsync()`; no `react-native-firebase`.

## API

```
POST /send
Authorization: Bearer <RELAY_BEARER_TOKEN>
Content-Type: application/json

{ "token": "<device token>", "platform": "android" | "ios",
  "title": "...", "body": "...", "data": { ... } }
```

Returns `201` on enqueue. `GET /healthz` → `200`.

## Emitted payloads (Expo-compatible)

**Android (FCM, data-only):**
```json
{ "message": { "token": "<fcm>", "data": {
  "title": "<title>", "message": "<body>",
  "body": "<JSON.stringify(data)>", "channelId": "default" } } }
```
`data.title` → shown title, `data.message` → shown body, `data.body` → JSON →
`content.data` in JS. (Verified against expo-notifications' `RemoteNotificationContent` parser.)

**iOS (APNs):**
```json
{ "aps": { "alert": { "title": "<title>", "body": "<body>" } },
  "body": "<JSON.stringify(data)>" }
```
> iOS follows Expo's documented direct-APNs format but is **unverified on a device**.

## Configuration (env vars)

| Var | Purpose |
|---|---|
| `PORT` | Listen port (Cloud Run sets this; default `8080`) |
| `RELAY_BEARER_TOKEN` | Shared secret the plugin must send as `Authorization: Bearer` |
| `FIREBASE_CREDENTIALS_FILE` | Path to the Firebase Admin service-account JSON. Optional — omit when using ADC (below). |
| `FCM_USE_ADC` | Set `true` to enable FCM via Application Default Credentials — Cloud Run running **as** the `firebase-adminsdk` service account, no key file or secret needed. |
| `APNS_KEY_FILE` | Path to the APNs `.p8` auth key (enables iOS) |
| `APNS_KEY_ID` | APNs key ID |
| `APNS_TEAM_ID` | Apple developer team ID |
| `APNS_BUNDLE_ID` | App bundle ID (APNs topic) |
| `APNS_PRODUCTION` | `"true"` for production APNs, otherwise sandbox |

Android and iOS are each enabled only when their credentials are present, so you
can run Android-only by omitting the `APNS_*` vars.

## Firebase version compatibility

The relay's **Firebase Admin SDK** (`firebase.google.com/go/v4`, server-side,
*sends*) and the app's **Firebase client SDK** (`com.google.firebase:firebase-messaging`,
in `expo-notifications`, *receives*) are **separate products with separate
version schemes and do not need to match.** They interoperate through the stable
**FCM HTTP v1 API**.

What must match:
- **Same Firebase project** — the relay's service-account JSON must be for the
  same project as the app's `google-services.json` (the FCM token is scoped to
  that project's Sender ID).
- The Admin SDK must use **FCM v1** — `v4.x` does.

## Secrets & deploy (Cloud Run)

Credentials are **never** committed or baked into the image. Store them in
**Google Secret Manager**, mount them into the Cloud Run service at runtime, and
point the `*_FILE` env vars at the mount paths. Grant the service account
`roles/secretmanager.secretAccessor` on those secrets only.

- `FIREBASE_CREDENTIALS_FILE` → mounted Firebase Admin JSON
- `APNS_KEY_FILE` → mounted `.p8`
- `RELAY_BEARER_TOKEN` → secret env var
- Map `push.sysaru.app` to the Cloud Run service after first deploy.

## Privacy posture

The relay logs **no device tokens and no notification content** — only startup
state and platform-tagged errors. It holds credentials only in memory (from the
mounted files); a repo or image leak exposes nothing.

## Build

```
go mod tidy   # resolves go.sum (latest stable deps)
go build ./...
```
