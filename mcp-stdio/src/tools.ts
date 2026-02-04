/**
 * GoWA MCP Tools Definition
 * Complete coverage of GoWA REST API endpoints
 */

import { Tool } from "@modelcontextprotocol/sdk/types.js";

// Tool definitions organized by category
export const tools: Tool[] = [
  // ============================================================================
  // APP / CONNECTION
  // ============================================================================
  {
    name: "app_login",
    description: "Login to WhatsApp server and get QR code for pairing",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "app_login_with_code",
    description: "Login with pairing code instead of QR code",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number to pair with (e.g., 5511999999999)" },
      },
      required: ["phone"],
    },
  },
  {
    name: "app_logout",
    description: "Logout from WhatsApp and remove session data",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "app_reconnect",
    description: "Reconnect to WhatsApp server",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "app_status",
    description: "Get current connection status (is_connected, is_logged_in)",
    inputSchema: { type: "object", properties: {} },
  },

  // ============================================================================
  // DEVICE MANAGEMENT
  // ============================================================================
  {
    name: "list_devices",
    description: "List all registered WhatsApp devices with connection status",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "add_device",
    description: "Add a new device slot for multi-device management",
    inputSchema: {
      type: "object",
      properties: {
        device_id: { type: "string", description: "Custom device ID (optional, auto-generated if not provided)" },
      },
    },
  },
  {
    name: "get_device_info",
    description: "Get detailed information about a specific device",
    inputSchema: {
      type: "object",
      properties: {
        device_id: { type: "string", description: "Device ID" },
      },
      required: ["device_id"],
    },
  },
  {
    name: "remove_device",
    description: "Remove a device from the server",
    inputSchema: {
      type: "object",
      properties: {
        device_id: { type: "string", description: "Device ID to remove" },
      },
      required: ["device_id"],
    },
  },
  {
    name: "device_login",
    description: "Initiate QR code login for a specific device",
    inputSchema: {
      type: "object",
      properties: {
        device_id: { type: "string", description: "Device ID" },
      },
      required: ["device_id"],
    },
  },
  {
    name: "device_login_with_code",
    description: "Initiate pairing code login for a specific device",
    inputSchema: {
      type: "object",
      properties: {
        device_id: { type: "string", description: "Device ID" },
        phone: { type: "string", description: "Phone number to pair with" },
      },
      required: ["device_id", "phone"],
    },
  },
  {
    name: "device_logout",
    description: "Logout a specific device from WhatsApp",
    inputSchema: {
      type: "object",
      properties: {
        device_id: { type: "string", description: "Device ID" },
      },
      required: ["device_id"],
    },
  },
  {
    name: "device_reconnect",
    description: "Reconnect a specific device to WhatsApp",
    inputSchema: {
      type: "object",
      properties: {
        device_id: { type: "string", description: "Device ID" },
      },
      required: ["device_id"],
    },
  },
  {
    name: "device_status",
    description: "Get connection status of a specific device",
    inputSchema: {
      type: "object",
      properties: {
        device_id: { type: "string", description: "Device ID" },
      },
      required: ["device_id"],
    },
  },

  // ============================================================================
  // USER / CONTACTS
  // ============================================================================
  {
    name: "check_number",
    description: "Check if a phone number is registered on WhatsApp",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number with country code (e.g., 5511999999999)" },
      },
      required: ["phone"],
    },
  },
  {
    name: "get_user_info",
    description: "Get information about a WhatsApp user",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number or JID" },
      },
      required: ["phone"],
    },
  },
  {
    name: "get_user_avatar",
    description: "Get the profile picture URL of a WhatsApp user",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number or JID" },
        is_preview: { type: "boolean", description: "Get preview/thumbnail version" },
      },
      required: ["phone"],
    },
  },
  {
    name: "get_business_profile",
    description: "Get business profile information for a WhatsApp business account",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number or JID of business account" },
      },
      required: ["phone"],
    },
  },
  {
    name: "get_contacts",
    description: "Get list of all contacts from the device",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "get_my_groups",
    description: "Get all groups that the authenticated user has joined (max 500)",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "get_my_newsletters",
    description: "Get all newsletters/channels that the user follows",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "get_my_privacy",
    description: "Get current privacy settings",
    inputSchema: { type: "object", properties: {} },
  },
  {
    name: "change_push_name",
    description: "Change the display name (push name) shown to others",
    inputSchema: {
      type: "object",
      properties: {
        push_name: { type: "string", description: "New display name" },
      },
      required: ["push_name"],
    },
  },

  // ============================================================================
  // SEND MESSAGES
  // ============================================================================
  {
    name: "send_message",
    description: "Send a text message to a WhatsApp number or group",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number/JID (e.g., 5511999999999 or group@g.us)" },
        message: { type: "string", description: "Message text to send" },
        reply_message_id: { type: "string", description: "Message ID to reply to (optional)" },
        mentions: { type: "array", items: { type: "string" }, description: "Phone numbers to mention, or '@everyone'" },
      },
      required: ["phone", "message"],
    },
  },
  {
    name: "send_image",
    description: "Send an image to a WhatsApp number or group",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number or JID" },
        image_url: { type: "string", description: "URL of the image to send" },
        caption: { type: "string", description: "Caption for the image" },
        view_once: { type: "boolean", description: "Send as view-once message" },
        compress: { type: "boolean", description: "Compress the image" },
      },
      required: ["phone", "image_url"],
    },
  },
  {
    name: "send_video",
    description: "Send a video to a WhatsApp number or group",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number or JID" },
        video_url: { type: "string", description: "URL of the video to send" },
        caption: { type: "string", description: "Caption for the video" },
        view_once: { type: "boolean", description: "Send as view-once message" },
        compress: { type: "boolean", description: "Compress the video" },
      },
      required: ["phone", "video_url"],
    },
  },
  {
    name: "send_audio",
    description: "Send an audio message to a WhatsApp number or group",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number or JID" },
        audio_url: { type: "string", description: "URL of the audio file" },
      },
      required: ["phone", "audio_url"],
    },
  },
  {
    name: "send_file",
    description: "Send a document/file to a WhatsApp number or group",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number or JID" },
        file_url: { type: "string", description: "URL of the file to send" },
        caption: { type: "string", description: "Caption for the file" },
      },
      required: ["phone", "file_url"],
    },
  },
  {
    name: "send_sticker",
    description: "Send a sticker (auto-converts to WebP format)",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number or JID" },
        sticker_url: { type: "string", description: "URL of the sticker image" },
      },
      required: ["phone", "sticker_url"],
    },
  },
  {
    name: "send_contact",
    description: "Send a contact card to a WhatsApp number or group",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number to send to" },
        contact_name: { type: "string", description: "Contact display name" },
        contact_phone: { type: "string", description: "Contact phone number" },
      },
      required: ["phone", "contact_name", "contact_phone"],
    },
  },
  {
    name: "send_location",
    description: "Send a location to a WhatsApp number or group",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number to send to" },
        latitude: { type: "string", description: "Latitude coordinate" },
        longitude: { type: "string", description: "Longitude coordinate" },
      },
      required: ["phone", "latitude", "longitude"],
    },
  },
  {
    name: "send_link",
    description: "Send a link with preview to a WhatsApp number or group",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number to send to" },
        link: { type: "string", description: "URL to send" },
        caption: { type: "string", description: "Caption for the link" },
      },
      required: ["phone", "link"],
    },
  },
  {
    name: "send_poll",
    description: "Send a poll/vote to a WhatsApp number or group",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number to send to" },
        question: { type: "string", description: "Poll question" },
        options: { type: "array", items: { type: "string" }, description: "Poll options" },
        max_answer: { type: "integer", description: "Maximum number of answers allowed" },
      },
      required: ["phone", "question", "options", "max_answer"],
    },
  },
  {
    name: "send_presence",
    description: "Set presence status (online/offline)",
    inputSchema: {
      type: "object",
      properties: {
        type: { type: "string", enum: ["available", "unavailable"], description: "Presence type" },
      },
      required: ["type"],
    },
  },
  {
    name: "send_typing",
    description: "Send typing indicator (start/stop composing)",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Phone number or JID" },
        action: { type: "string", enum: ["start", "stop"], description: "Start or stop typing" },
      },
      required: ["phone", "action"],
    },
  },

  // ============================================================================
  // MESSAGE ACTIONS
  // ============================================================================
  {
    name: "revoke_message",
    description: "Revoke/unsend a message for everyone",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Chat JID" },
        message_id: { type: "string", description: "Message ID to revoke" },
      },
      required: ["phone", "message_id"],
    },
  },
  {
    name: "delete_message",
    description: "Delete a message (local only)",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Chat JID" },
        message_id: { type: "string", description: "Message ID to delete" },
      },
      required: ["phone", "message_id"],
    },
  },
  {
    name: "react_to_message",
    description: "React to a message with an emoji",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Chat JID" },
        message_id: { type: "string", description: "Message ID to react to" },
        emoji: { type: "string", description: "Emoji reaction (e.g., 👍, ❤️, 😂)" },
      },
      required: ["phone", "message_id", "emoji"],
    },
  },
  {
    name: "edit_message",
    description: "Edit a sent message (within 15 minutes)",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Chat JID" },
        message_id: { type: "string", description: "Message ID to edit" },
        message: { type: "string", description: "New message text" },
      },
      required: ["phone", "message_id", "message"],
    },
  },
  {
    name: "mark_as_read",
    description: "Mark a message as read",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Chat JID" },
        message_id: { type: "string", description: "Message ID to mark as read" },
      },
      required: ["phone", "message_id"],
    },
  },
  {
    name: "star_message",
    description: "Star/favorite a message",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Chat JID" },
        message_id: { type: "string", description: "Message ID to star" },
      },
      required: ["phone", "message_id"],
    },
  },
  {
    name: "unstar_message",
    description: "Remove star from a message",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Chat JID" },
        message_id: { type: "string", description: "Message ID to unstar" },
      },
      required: ["phone", "message_id"],
    },
  },
  {
    name: "download_media",
    description: "Download media (image/video/audio/document) from a message",
    inputSchema: {
      type: "object",
      properties: {
        phone: { type: "string", description: "Chat JID" },
        message_id: { type: "string", description: "Message ID containing media" },
      },
      required: ["phone", "message_id"],
    },
  },

  // ============================================================================
  // CHATS
  // ============================================================================
  {
    name: "list_chats",
    description: "Get list of chat conversations",
    inputSchema: {
      type: "object",
      properties: {
        limit: { type: "integer", description: "Max chats to return (default: 25, max: 100)" },
        offset: { type: "integer", description: "Number of chats to skip" },
        search: { type: "string", description: "Search chats by name" },
        has_media: { type: "boolean", description: "Filter chats with media" },
      },
    },
  },
  {
    name: "get_chat_messages",
    description: "Get messages from a specific chat",
    inputSchema: {
      type: "object",
      properties: {
        chat_jid: { type: "string", description: "Chat JID (e.g., 5511999999999@s.whatsapp.net)" },
        limit: { type: "integer", description: "Max messages to return (default: 50)" },
        offset: { type: "integer", description: "Number of messages to skip" },
        search: { type: "string", description: "Search messages by content" },
        media_only: { type: "boolean", description: "Only return media messages" },
        is_from_me: { type: "boolean", description: "Filter by sender (true=sent, false=received)" },
      },
      required: ["chat_jid"],
    },
  },
  {
    name: "label_chat",
    description: "Apply or remove a label from a chat",
    inputSchema: {
      type: "object",
      properties: {
        chat_jid: { type: "string", description: "Chat JID" },
        label_id: { type: "string", description: "Label ID" },
        label_name: { type: "string", description: "Label display name" },
        labeled: { type: "boolean", description: "Apply (true) or remove (false)" },
      },
      required: ["chat_jid", "label_id", "label_name", "labeled"],
    },
  },
  {
    name: "pin_chat",
    description: "Pin or unpin a chat to the top",
    inputSchema: {
      type: "object",
      properties: {
        chat_jid: { type: "string", description: "Chat JID" },
        pinned: { type: "boolean", description: "Pin (true) or unpin (false)" },
      },
      required: ["chat_jid", "pinned"],
    },
  },
  {
    name: "archive_chat",
    description: "Archive or unarchive a chat",
    inputSchema: {
      type: "object",
      properties: {
        chat_jid: { type: "string", description: "Chat JID" },
        archived: { type: "boolean", description: "Archive (true) or unarchive (false)" },
      },
      required: ["chat_jid", "archived"],
    },
  },
  {
    name: "set_disappearing_timer",
    description: "Set disappearing messages timer (0=off, 86400=24h, 604800=7d, 7776000=90d)",
    inputSchema: {
      type: "object",
      properties: {
        chat_jid: { type: "string", description: "Chat JID" },
        timer_seconds: { type: "integer", description: "Timer in seconds (0, 86400, 604800, 7776000)" },
      },
      required: ["chat_jid", "timer_seconds"],
    },
  },

  // ============================================================================
  // GROUPS
  // ============================================================================
  {
    name: "get_group_info",
    description: "Get information about a specific group",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID (e.g., 120363025982934543@g.us)" },
      },
      required: ["group_id"],
    },
  },
  {
    name: "create_group",
    description: "Create a new WhatsApp group",
    inputSchema: {
      type: "object",
      properties: {
        title: { type: "string", description: "Group name" },
        participants: { type: "array", items: { type: "string" }, description: "Phone numbers to add" },
      },
      required: ["title", "participants"],
    },
  },
  {
    name: "leave_group",
    description: "Leave a WhatsApp group",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID to leave" },
      },
      required: ["group_id"],
    },
  },
  {
    name: "get_group_participants",
    description: "Get list of participants in a group",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
      },
      required: ["group_id"],
    },
  },
  {
    name: "add_group_participants",
    description: "Add participants to a group",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        participants: { type: "array", items: { type: "string" }, description: "Phone numbers to add" },
      },
      required: ["group_id", "participants"],
    },
  },
  {
    name: "remove_group_participants",
    description: "Remove participants from a group",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        participants: { type: "array", items: { type: "string" }, description: "Phone numbers to remove" },
      },
      required: ["group_id", "participants"],
    },
  },
  {
    name: "promote_group_participants",
    description: "Promote participants to admin",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        participants: { type: "array", items: { type: "string" }, description: "Phone numbers to promote" },
      },
      required: ["group_id", "participants"],
    },
  },
  {
    name: "demote_group_participants",
    description: "Demote admins to regular members",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        participants: { type: "array", items: { type: "string" }, description: "Phone numbers to demote" },
      },
      required: ["group_id", "participants"],
    },
  },
  {
    name: "set_group_name",
    description: "Change group name (max 25 characters)",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        name: { type: "string", description: "New group name" },
      },
      required: ["group_id", "name"],
    },
  },
  {
    name: "set_group_topic",
    description: "Set or remove group description/topic",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        topic: { type: "string", description: "New topic (empty to remove)" },
      },
      required: ["group_id"],
    },
  },
  {
    name: "set_group_locked",
    description: "Lock/unlock group so only admins can modify info",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        locked: { type: "boolean", description: "Lock (true) or unlock (false)" },
      },
      required: ["group_id", "locked"],
    },
  },
  {
    name: "set_group_announce",
    description: "Enable/disable announce mode (only admins can send)",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        announce: { type: "boolean", description: "Enable (true) or disable (false)" },
      },
      required: ["group_id", "announce"],
    },
  },
  {
    name: "get_group_invite_link",
    description: "Get or reset group invite link",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        reset: { type: "boolean", description: "Reset existing link" },
      },
      required: ["group_id"],
    },
  },
  {
    name: "join_group_with_link",
    description: "Join a group using an invite link",
    inputSchema: {
      type: "object",
      properties: {
        link: { type: "string", description: "Group invite link (https://chat.whatsapp.com/...)" },
      },
      required: ["link"],
    },
  },
  {
    name: "get_group_info_from_link",
    description: "Get group info from invite link without joining",
    inputSchema: {
      type: "object",
      properties: {
        link: { type: "string", description: "Group invite link" },
      },
      required: ["link"],
    },
  },
  {
    name: "get_group_join_requests",
    description: "Get pending requests to join a group",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
      },
      required: ["group_id"],
    },
  },
  {
    name: "approve_group_join_request",
    description: "Approve participant request to join group",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        participants: { type: "array", items: { type: "string" }, description: "Phone numbers to approve" },
      },
      required: ["group_id", "participants"],
    },
  },
  {
    name: "reject_group_join_request",
    description: "Reject participant request to join group",
    inputSchema: {
      type: "object",
      properties: {
        group_id: { type: "string", description: "Group JID" },
        participants: { type: "array", items: { type: "string" }, description: "Phone numbers to reject" },
      },
      required: ["group_id", "participants"],
    },
  },
];

export default tools;
