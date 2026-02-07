/**
 * GoWA MCP Tool Handlers
 * Maps MCP tool calls to GoWA REST API requests
 */

// Configuration interface
export interface Config {
  gowaUrl: string;
  deviceId: string;
}

// HTTP helper for GoWA API calls
export async function gowaRequest(
  config: Config,
  method: string,
  path: string,
  body?: Record<string, unknown>
): Promise<unknown> {
  const url = `${config.gowaUrl}${path}`;
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };

  if (config.deviceId) {
    headers["X-Device-Id"] = config.deviceId;
  }

  const options: RequestInit = { method, headers };
  if (body && (method === "POST" || method === "PUT" || method === "PATCH")) {
    options.body = JSON.stringify(body);
  }

  const response = await fetch(url, options);
  const data = await response.json();

  if (!response.ok) {
    throw new Error(`GoWA API error: ${JSON.stringify(data)}`);
  }

  return data;
}

// Tool handler - maps tool name to API call
export async function handleTool(
  config: Config,
  name: string,
  args: Record<string, unknown>
): Promise<unknown> {
  const api = (method: string, path: string, body?: Record<string, unknown>) =>
    gowaRequest(config, method, path, body);

  switch (name) {
    // ========================================================================
    // APP / CONNECTION
    // ========================================================================
    case "app_login":
      return api("GET", "/app/login");

    case "app_login_with_code":
      return api("GET", `/app/login-with-code?phone=${args.phone}`);

    case "app_logout":
      return api("GET", "/app/logout");

    case "app_reconnect":
      return api("GET", "/app/reconnect");

    case "app_status":
      return api("GET", "/app/status");

    // ========================================================================
    // DEVICE MANAGEMENT
    // ========================================================================
    case "list_devices":
      return api("GET", "/devices");

    case "add_device":
      return api("POST", "/devices", args.device_id ? { device_id: args.device_id } : {});

    case "get_device_info":
      return api("GET", `/devices/${args.device_id}`);

    case "remove_device":
      return api("DELETE", `/devices/${args.device_id}`);

    case "device_login":
      return api("GET", `/devices/${args.device_id}/login`);

    case "device_login_with_code":
      return api("POST", `/devices/${args.device_id}/login/code?phone=${args.phone}`);

    case "device_logout":
      return api("POST", `/devices/${args.device_id}/logout`);

    case "device_reconnect":
      return api("POST", `/devices/${args.device_id}/reconnect`);

    case "device_status":
      return api("GET", `/devices/${args.device_id}/status`);

    // ========================================================================
    // USER / CONTACTS
    // ========================================================================
    case "check_number":
      return api("GET", `/user/check?phone=${args.phone}`);

    case "get_user_info":
      return api("GET", `/user/info?phone=${args.phone}`);

    case "get_user_avatar":
      const avatarParams = new URLSearchParams({ phone: String(args.phone) });
      if (args.is_preview) avatarParams.set("is_preview", "true");
      return api("GET", `/user/avatar?${avatarParams}`);

    case "get_business_profile":
      return api("GET", `/user/business-profile?phone=${args.phone}`);

    case "get_contacts":
      return api("GET", "/user/my/contacts");

    case "get_my_groups":
      return api("GET", "/user/my/groups");

    case "get_my_newsletters":
      return api("GET", "/user/my/newsletters");

    case "get_my_privacy":
      return api("GET", "/user/my/privacy");

    case "change_push_name":
      return api("POST", "/user/pushname", { push_name: args.push_name });

    // ========================================================================
    // SEND MESSAGES
    // ========================================================================
    case "send_message":
      return api("POST", "/send/message", {
        phone: args.phone,
        message: args.message,
        reply_message_id: args.reply_message_id,
        mentions: args.mentions,
      });

    case "send_image":
      return api("POST", "/send/image", {
        phone: args.phone,
        image_url: args.image_url,
        caption: args.caption || "",
        view_once: args.view_once || false,
        compress: args.compress || false,
      });

    case "send_video":
      return api("POST", "/send/video", {
        phone: args.phone,
        video_url: args.video_url,
        caption: args.caption || "",
        view_once: args.view_once || false,
        compress: args.compress || false,
      });

    case "send_audio":
      return api("POST", "/send/audio", {
        phone: args.phone,
        audio_url: args.audio_url,
      });

    case "send_file":
      return api("POST", "/send/file", {
        phone: args.phone,
        file_url: args.file_url,
        caption: args.caption || "",
      });

    case "send_sticker":
      return api("POST", "/send/sticker", {
        phone: args.phone,
        sticker_url: args.sticker_url,
      });

    case "send_contact":
      return api("POST", "/send/contact", {
        phone: args.phone,
        contact_name: args.contact_name,
        contact_phone: args.contact_phone,
      });

    case "send_location":
      return api("POST", "/send/location", {
        phone: args.phone,
        latitude: args.latitude,
        longitude: args.longitude,
      });

    case "send_link":
      return api("POST", "/send/link", {
        phone: args.phone,
        link: args.link,
        caption: args.caption || "",
      });

    case "send_poll":
      return api("POST", "/send/poll", {
        phone: args.phone,
        question: args.question,
        options: args.options,
        max_answer: args.max_answer,
      });

    case "send_presence":
      return api("POST", "/send/presence", { type: args.type });

    case "send_typing":
      return api("POST", "/send/chat-presence", {
        phone: args.phone,
        action: args.action,
      });

    // ========================================================================
    // MESSAGE ACTIONS
    // ========================================================================
    case "revoke_message":
      return api("POST", `/message/${args.message_id}/revoke`, { phone: args.phone });

    case "delete_message":
      return api("POST", `/message/${args.message_id}/delete`, { phone: args.phone });

    case "react_to_message":
      return api("POST", `/message/${args.message_id}/reaction`, {
        phone: args.phone,
        emoji: args.emoji,
      });

    case "edit_message":
      return api("POST", `/message/${args.message_id}/update`, {
        phone: args.phone,
        message: args.message,
      });

    case "mark_as_read":
      return api("POST", `/message/${args.message_id}/read`, { phone: args.phone });

    case "star_message":
      return api("POST", `/message/${args.message_id}/star`, { phone: args.phone });

    case "unstar_message":
      return api("POST", `/message/${args.message_id}/unstar`, { phone: args.phone });

    case "download_media":
      return api("GET", `/message/${args.message_id}/download?phone=${args.phone}`);

    // ========================================================================
    // CHATS
    // ========================================================================
    case "list_chats":
      const chatParams = new URLSearchParams();
      if (args.limit) chatParams.set("limit", String(args.limit));
      if (args.offset) chatParams.set("offset", String(args.offset));
      if (args.search) chatParams.set("search", String(args.search));
      if (args.has_media) chatParams.set("has_media", "true");
      return api("GET", `/chats?${chatParams}`);

    case "get_chat_messages":
      const msgParams = new URLSearchParams();
      if (args.limit) msgParams.set("limit", String(args.limit));
      if (args.offset) msgParams.set("offset", String(args.offset));
      if (args.search) msgParams.set("search", String(args.search));
      if (args.media_only) msgParams.set("media_only", "true");
      if (args.is_from_me !== undefined) msgParams.set("is_from_me", String(args.is_from_me));
      return api("GET", `/chat/${args.chat_jid}/messages?${msgParams}`);

    case "label_chat":
      return api("POST", `/chat/${args.chat_jid}/label`, {
        label_id: args.label_id,
        label_name: args.label_name,
        labeled: args.labeled,
      });

    case "pin_chat":
      return api("POST", `/chat/${args.chat_jid}/pin`, { pinned: args.pinned });

    case "archive_chat":
      return api("POST", `/chat/${args.chat_jid}/archive`, { archived: args.archived });

    case "set_disappearing_timer":
      return api("POST", `/chat/${args.chat_jid}/disappearing`, {
        timer_seconds: args.timer_seconds,
      });

    // ========================================================================
    // GROUPS
    // ========================================================================
    case "get_group_info":
      return api("GET", `/group/info?group_id=${args.group_id}`);

    case "create_group":
      return api("POST", "/group", {
        title: args.title,
        participants: args.participants,
      });

    case "leave_group":
      return api("POST", "/group/leave", { group_id: args.group_id });

    case "get_group_participants":
      return api("GET", `/group/participants?group_id=${args.group_id}`);

    case "add_group_participants":
      return api("POST", "/group/participants", {
        group_id: args.group_id,
        participants: args.participants,
      });

    case "remove_group_participants":
      return api("POST", "/group/participants/remove", {
        group_id: args.group_id,
        participants: args.participants,
      });

    case "promote_group_participants":
      return api("POST", "/group/participants/promote", {
        group_id: args.group_id,
        participants: args.participants,
      });

    case "demote_group_participants":
      return api("POST", "/group/participants/demote", {
        group_id: args.group_id,
        participants: args.participants,
      });

    case "set_group_name":
      return api("POST", "/group/name", {
        group_id: args.group_id,
        name: args.name,
      });

    case "set_group_topic":
      return api("POST", "/group/topic", {
        group_id: args.group_id,
        topic: args.topic || "",
      });

    case "set_group_locked":
      return api("POST", "/group/locked", {
        group_id: args.group_id,
        locked: args.locked,
      });

    case "set_group_announce":
      return api("POST", "/group/announce", {
        group_id: args.group_id,
        announce: args.announce,
      });

    case "get_group_invite_link":
      const linkParams = new URLSearchParams({ group_id: String(args.group_id) });
      if (args.reset) linkParams.set("reset", "true");
      return api("GET", `/group/invite-link?${linkParams}`);

    case "join_group_with_link":
      return api("POST", "/group/join-with-link", { link: args.link });

    case "get_group_info_from_link":
      return api("GET", `/group/info-from-link?link=${encodeURIComponent(String(args.link))}`);

    case "get_group_join_requests":
      return api("GET", `/group/participant-requests?group_id=${args.group_id}`);

    case "approve_group_join_request":
      return api("POST", "/group/participant-requests/approve", {
        group_id: args.group_id,
        participants: args.participants,
      });

    case "reject_group_join_request":
      return api("POST", "/group/participant-requests/reject", {
        group_id: args.group_id,
        participants: args.participants,
      });

    default:
      throw new Error(`Unknown tool: ${name}`);
  }
}

export default handleTool;
