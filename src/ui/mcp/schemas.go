package mcp

// Raw JSON Schemas for the consolidated tools. mcp-go validates tool calls
// against these before handlers run (santhosh-tekuri/jsonschema/v6), so the
// allOf/if/then conditionals are enforced, not advisory.

const sendSchema = `{
  "type": "object",
  "required": ["type", "phone"],
  "properties": {
    "type": {"type": "string", "enum": ["text","image","video","audio","document","sticker","location","contact","poll","link","forward"], "description": "Kind of message to send"},
    "phone": {"type": "string", "description": "Destination phone number or group JID"},
    "device_id": {"type": "string", "description": "Act as this device instead of the connection default (X-Device-Id header)"},
    "is_forwarded": {"type": "boolean", "description": "Mark the message as forwarded (default false)"},
    "message": {"type": "string", "description": "type=text: the text body"},
    "reply_message_id": {"type": "string", "description": "type=text: message ID to reply to"},
    "mentions": {"type": "array", "items": {"type": "string"}, "description": "type=text: ghost mentions; \"@everyone\" mentions all group participants"},
    "image_url": {"type": "string", "description": "type=image: URL of the image (fetched server-side)"},
    "caption": {"type": "string", "description": "image/video/document/link: caption text"},
    "view_once": {"type": "boolean", "description": "image/video: view-once message (default false)"},
    "compress": {"type": "boolean", "description": "image (default true) / video (default false): re-encode before sending"},
    "hd": {"type": "boolean", "description": "image/video: send HD without upscaling (image: 2560px max edge; video: H.264 8-bit YUV 4:2:0 at CRF 23, capped at 1280x1280); overrides compress when true (default false)"},
    "video_url": {"type": "string", "description": "type=video: URL of the video (mp4/mkv/avi, fetched server-side)"},
    "gif_playback": {"type": "boolean", "description": "type=video: play as looping GIF (default false)"},
    "audio_url": {"type": "string", "description": "type=audio: URL of the audio file (fetched server-side)"},
    "ptt": {"type": "boolean", "description": "type=audio: send as voice note (requires ffmpeg server-side, default false)"},
    "file_url": {"type": "string", "description": "type=document: URL of the file; MIME type and filename derived server-side"},
    "sticker_url": {"type": "string", "description": "type=sticker: URL of an image to convert to a WebP sticker"},
    "latitude": {"type": "string", "description": "type=location: latitude as string"},
    "longitude": {"type": "string", "description": "type=location: longitude as string"},
    "contact_name": {"type": "string", "description": "type=contact: contact display name"},
    "contact_phone": {"type": "string", "description": "type=contact: contact phone number"},
    "question": {"type": "string", "description": "type=poll: the poll question"},
    "options": {"type": "array", "items": {"type": "string"}, "minItems": 2, "description": "type=poll: poll options (min 2)"},
    "max_answer": {"type": "integer", "description": "type=poll: max selectable options (default 1)"},
    "link": {"type": "string", "description": "type=link: the URL to send"},
    "message_id": {"type": "string", "description": "type=forward: source message ID from chat storage"},
    "duration": {"type": "integer", "description": "type=forward: disappearing duration seconds (0, 86400, 604800, 7776000)"},
    "force_reupload": {"type": "boolean", "description": "type=forward: re-upload media instead of reusing references (default false)"}
  },
  "allOf": [
    {"if": {"properties": {"type": {"const": "text"}}},     "then": {"required": ["message"]}},
    {"if": {"properties": {"type": {"const": "image"}}},    "then": {"required": ["image_url"]}},
    {"if": {"properties": {"type": {"const": "video"}}},    "then": {"required": ["video_url"]}},
    {"if": {"properties": {"type": {"const": "audio"}}},    "then": {"required": ["audio_url"]}},
    {"if": {"properties": {"type": {"const": "document"}}}, "then": {"required": ["file_url"]}},
    {"if": {"properties": {"type": {"const": "sticker"}}},  "then": {"required": ["sticker_url"]}},
    {"if": {"properties": {"type": {"const": "location"}}}, "then": {"required": ["latitude", "longitude"]}},
    {"if": {"properties": {"type": {"const": "contact"}}},  "then": {"required": ["contact_name", "contact_phone"]}},
    {"if": {"properties": {"type": {"const": "poll"}}},     "then": {"required": ["question", "options"]}},
    {"if": {"properties": {"type": {"const": "link"}}},     "then": {"required": ["link", "caption"]}},
    {"if": {"properties": {"type": {"const": "forward"}}},  "then": {"required": ["message_id"]}}
  ]
}`

const messageSchema = `{
  "type": "object",
  "required": ["action", "phone", "message_id"],
  "properties": {
    "action": {"type": "string", "enum": ["react","edit","revoke","delete","mark_read","mark_played","star","unstar","download_media"], "description": "Operation on an existing message. revoke = delete for everyone (destructive); delete = delete for me only; mark_played sends the played receipt for an incoming audio message; download_media returns the local file path"},
    "phone": {"type": "string", "description": "Phone number or group JID of the chat containing the message"},
    "message_id": {"type": "string", "description": "The WhatsApp message ID"},
    "device_id": {"type": "string", "description": "Act as this device instead of the connection default"},
    "emoji": {"type": "string", "description": "action=react: emoji to react with; empty string removes the reaction"},
    "message": {"type": "string", "description": "action=edit: replacement text (works ~15 minutes after send)"}
  },
  "allOf": [
    {"if": {"properties": {"action": {"const": "edit"}}}, "then": {"required": ["message"]}}
  ]
}`

const chatSchema = `{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {"type": "string", "enum": ["list_chats","list_contacts","get_messages","archive"], "description": "Chat/contact query or archive toggle"},
    "device_id": {"type": "string", "description": "Act as this device instead of the connection default"},
    "chat_jid": {"type": "string", "description": "get_messages/archive: chat JID (e.g. 628@s.whatsapp.net or group@g.us)"},
    "limit": {"type": "integer", "description": "list_chats (default 25) / get_messages (default 50): max rows"},
    "offset": {"type": "integer", "description": "list_chats/get_messages: rows to skip (default 0)"},
    "search": {"type": "string", "description": "list_chats: filter by chat name; get_messages: full-text search"},
    "has_media": {"type": "boolean", "description": "list_chats: only chats containing media"},
    "start_time": {"type": "string", "description": "get_messages: only messages after this RFC3339 timestamp"},
    "end_time": {"type": "string", "description": "get_messages: only messages before this RFC3339 timestamp"},
    "media_only": {"type": "boolean", "description": "get_messages: only media messages"},
    "is_from_me": {"type": "boolean", "description": "get_messages: filter by sender (true = sent by me)"},
    "archived": {"type": "boolean", "description": "archive: true to archive, false to unarchive"}
  },
  "allOf": [
    {"if": {"properties": {"action": {"const": "get_messages"}}}, "then": {"required": ["chat_jid"]}},
    {"if": {"properties": {"action": {"const": "archive"}}},      "then": {"required": ["chat_jid", "archived"]}}
  ]
}`

const groupSchema = `{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {"type": "string", "enum": ["create","join_with_link","leave","info","participants","add_participants","remove_participants","promote","demote","invite_link","set_name","set_topic","set_settings","join_requests","manage_join_requests"], "description": "Group operation"},
    "device_id": {"type": "string", "description": "Act as this device instead of the connection default"},
    "group_id": {"type": "string", "description": "Group JID or numeric ID (all actions except create/join_with_link)"},
    "title": {"type": "string", "description": "create: group subject"},
    "participants": {"type": "array", "items": {"type": "string"}, "description": "create (optional) / add_participants / remove_participants / promote / demote / manage_join_requests: phone numbers without suffix"},
    "invite_link": {"type": "string", "description": "join_with_link: WhatsApp invite link"},
    "reset": {"type": "boolean", "description": "invite_link: revoke and regenerate the link (default false)"},
    "name": {"type": "string", "description": "set_name: new group name"},
    "topic": {"type": "string", "description": "set_topic: new topic/description"},
    "announce": {"type": "boolean", "description": "set_settings: true = only admins can send"},
    "locked": {"type": "boolean", "description": "set_settings: true = only admins can edit group info"},
    "request_action": {"type": "string", "enum": ["approve","reject"], "description": "manage_join_requests: what to do with the listed requesters"}
  },
  "allOf": [
    {"if": {"properties": {"action": {"const": "create"}}},         "then": {"required": ["title"]}},
    {"if": {"properties": {"action": {"const": "join_with_link"}}}, "then": {"required": ["invite_link"]}},
    {"if": {"properties": {"action": {"enum": ["leave","info","participants","invite_link","join_requests"]}}}, "then": {"required": ["group_id"]}},
    {"if": {"properties": {"action": {"enum": ["add_participants","remove_participants","promote","demote"]}}}, "then": {"required": ["group_id", "participants"]}},
    {"if": {"properties": {"action": {"const": "set_name"}}},  "then": {"required": ["group_id", "name"]}},
    {"if": {"properties": {"action": {"const": "set_topic"}}}, "then": {"required": ["group_id", "topic"]}},
    {"if": {"properties": {"action": {"const": "set_settings"}}}, "then": {"required": ["group_id"], "anyOf": [{"required": ["announce"]}, {"required": ["locked"]}]}},
    {"if": {"properties": {"action": {"const": "manage_join_requests"}}}, "then": {"required": ["group_id", "participants", "request_action"]}}
  ]
}`

const appSchema = `{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {"type": "string", "enum": ["status","login_qr","login_code","logout","reconnect"], "description": "Connection/session operation. login_qr returns a QR image; logout clears stored credentials (destructive)"},
    "device_id": {"type": "string", "description": "Act as this device instead of the connection default"},
    "phone": {"type": "string", "description": "login_code: phone number in international format (e.g. +628123456789)"}
  },
  "allOf": [
    {"if": {"properties": {"action": {"const": "login_code"}}}, "then": {"required": ["phone"]}}
  ]
}`
