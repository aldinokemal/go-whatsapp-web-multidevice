# Experimental interactive messages

`POST /send/interactive` uses existing Basic Auth and `X-Device-Id` device
selection. This is an immediate-only REST endpoint, not a scheduled-send or MCP
operation. It uses Whatsmeow protobuf messages and binary business nodes, not
XML in visible message text. No bot/AI marker is added.

## Buttons

```sh
curl --user "$GOWA_USER:$GOWA_PASSWORD" \
  -H 'Content-Type: application/json' -H "X-Device-Id: $DEVICE_ID" \
  "$GOWA_URL/send/interactive" --data '{
    "phone":"15551234567",
    "body":"Confirm your order?",
    "buttons":[
      {"name":"quick_reply","params":{"display_text":"Confirm","id":"confirm"}},
      {"name":"quick_reply","params":{"display_text":"Cancel","id":"cancel"}}
    ]
  }'
```

| Button name | Params |
| --- | --- |
| `quick_reply` | `display_text`, `id` |
| `cta_url` | `display_text`, absolute HTTP(S) `url` |
| `cta_copy` | `display_text`, `copy_code` |
| `cta_call` | `display_text`, `phone_number` |
| `single_select` | `title`, nonempty `sections` array |
| `send_location` | `{}`; must be the only button, outside a carousel |

Selection parameters:

```json
{"title":"Choose","sections":[{"title":"Options","rows":[{"id":"a","title":"Option A"},{"id":"b","title":"Option B"}]}]}
```

For location requests, send `buttons:[{"name":"send_location","params":{}}]`
with `phone` and `body`. This uses the tested unwrapped native-flow payload,
explicit non-media header, message context, no native-flow MessageVersion,
and a dedicated v2 send_location business node. It is NOT the standalone
RequestLocationMessage protobuf, which did not render in our tests. Location
sharing always requires the recipient's action and consent.

`phone` can also be a full user/group JID. Groups require membership and posting
permission; rendering and actions must be verified separately for each client.

## Image carousels

Use 2–10 `cards` instead of top-level `buttons`:

```json
{
  "phone":"15551234567",
  "body":"Choose a product",
  "cards":[
    {"title":"A","body":"Product A","image_base64":"<raw JPEG/PNG base64>","buttons":[{"name":"quick_reply","params":{"display_text":"Choose A","id":"choose_a"}}]},
    {"title":"B","body":"Product B","image_base64":"<raw JPEG/PNG base64>","buttons":[{"name":"quick_reply","params":{"display_text":"Choose B","id":"choose_b"}}]}
  ]
}
```

Replace image placeholders with real raw base64, without a data-URL prefix.
JPEG/PNG only, up to 4096 pixels per side and 2,800,000 encoded characters per
image. Images are resized, JPEG-encoded, thumbnailed, and uploaded to WhatsApp.
Overall HTTP body limits also apply: use small images, especially for 10 cards.
No remote image fetching or new dependencies are introduced.

## Limits and evidence

Body: required, 4096 bytes maximum; footer: 256 bytes. Each message/card accepts
1–10 buttons; each params object up to 16,384 bytes. Card title/footer: 256 bytes.
The API's validation limits do not guarantee recipient-client compatibility.
The existing device scope and asynchronous sent-message persistence are retained.

Manual Android tests on a downstream v9.7.0 deployment confirmed reply, URL,
copy, call, selection, image carousel, and unwrapped location behavior. Provided
screenshots also show mixed buttons, carousel replies, and a 10-card carousel.
They are evidence from a particular account/client, not universal compatibility
or independent maintainer verification. iOS/Web versions were not verified.

Known issue: screenshots show clock/pending icons on individual carousel cards,
even when the parent message has checkmarks and replies are possible. This is
unresolved; do not interpret a send acknowledgment as a delivery/read receipt.

Successful responses contain `results.message_id` and explicitly say recipient
rendering is unverified. Use existing incoming webhooks for replies and
`message.ack` events for receipts; see [webhook-payload.md](webhook-payload.md).
Blind retries after an ambiguous timeout can duplicate messages.

Not included: failed inline Flows, standalone location requests, legacy template
experiments, official template registration, catalogs or native payments.
This unofficial protocol can change and carries account-restriction risk.
