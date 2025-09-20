/**
 * WhatsApp API MultiDevice
 * 6.10.0
 * DO NOT MODIFY - This file has been generated using oazapfts.
 * See https://www.npmjs.com/package/oazapfts
 */
import * as Oazapfts from "@oazapfts/runtime";
import * as QS from "@oazapfts/runtime/query";
export const defaults: Oazapfts.Defaults<Oazapfts.CustomHeaders> = {
    headers: {},
    baseUrl: "http://localhost:3000",
};
const oazapfts = Oazapfts.runtime(defaults);
export const servers = {
    server1: "http://localhost:3000"
};
export type LoginResponse = {
    code?: string;
    message?: string;
    results?: {
        qr_duration?: number;
        qr_link?: string;
    };
};
export type ErrorInternalServer = {
    /** SYSTEM_CODE_ERROR */
    code?: string;
    /** Detail error message */
    message?: string;
    /** additional data */
    results?: object;
};
export type LoginWithCodeResponse = {
    code?: string;
    message?: string;
    results?: {
        pair_code?: string;
    };
};
export type GenericResponse = {
    code?: string;
    message?: string;
    results?: string;
};
export type DeviceResponse = {
    code?: string;
    message?: string;
    results?: {
        name?: string;
        device?: string;
    }[];
};
export type UserInfoResponse = {
    code: string;
    message: string;
    results: {
        verified_name?: string;
        status?: string;
        picture_id?: string;
        devices?: {
            User?: string;
            Agent?: number;
            Device?: string;
            Server?: string;
            AD?: boolean;
        }[];
    };
};
export type ErrorBadRequest = {
    /** HTTP Status Code */
    code?: string;
    /** Detail error message */
    message?: string;
    /** additional data */
    results?: object;
};
export type UserAvatarResponse = {
    code?: string;
    message?: string;
    results?: {
        url?: string;
        id?: string;
        "type"?: string;
    };
};
export type UserPrivacyResponse = {
    code?: string;
    message?: string;
    results?: {
        group_add?: string;
        last_seen?: string;
        status?: string;
        profile?: string;
        read_receipts?: string;
    };
};
export type UserGroupResponse = {
    code?: string;
    message?: string;
    results?: {
        data?: {
            JID?: string;
            OwnerJID?: string;
            Name?: string;
            NameSetAt?: string;
            NameSetBy?: string;
            GroupCreated?: string;
            ParticipantVersionID?: string;
            Participants?: {
                JID?: string;
                IsAdmin?: boolean;
                IsSuperAdmin?: boolean;
                Error?: number;
            }[];
        }[];
    };
};
export type Newsletter = {
    id?: string;
    state?: {
        "type"?: string;
    };
    thread_metadata?: {
        creation_time?: string;
        invite?: string;
        name?: {
            text?: string;
            id?: string;
            update_time?: string;
        };
        description?: {
            text?: string;
            id?: string;
            update_time?: string;
        };
        subscribers_count?: string;
        verification?: string;
        picture?: {
            url?: string;
            id?: string;
            "type"?: string;
            direct_path?: string;
        };
        preview?: {
            url?: string;
            id?: string;
            "type"?: string;
            direct_path?: string;
        };
        settings?: {
            reaction_codes?: {
                value?: string;
            };
        };
    };
    viewer_metadata?: {
        mute?: string;
        role?: string;
    };
};
export type NewsletterResponse = {
    code?: string;
    message?: string;
    results?: {
        data?: Newsletter[];
    };
};
export type MyListContacts = {
    jid?: string;
    name?: string;
};
export type MyListContactsResponse = {
    code?: string;
    message?: string;
    results?: {
        data?: MyListContacts[];
    };
};
export type UserCheckResponse = {
    code?: string;
    message?: string;
    results?: {
        is_on_whatsapp?: boolean;
    };
};
export type BusinessProfileResponse = {
    code?: string;
    message?: string;
    results?: {
        /** Business account JID */
        jid?: string;
        /** Business email address */
        email?: string;
        /** Business physical address */
        address?: string;
        /** Business categories */
        categories?: {
            id?: string;
            name?: string;
        }[];
        /** Additional profile options */
        profile_options?: object;
        /** Business hours timezone */
        business_hours_timezone?: string;
        /** Business operating hours */
        business_hours?: {
            day_of_week?: string;
            mode?: string;
            open_time?: string;
            close_time?: string;
        }[];
    };
};
export type SendResponse = {
    code?: string;
    message?: string;
    results?: {
        message_id?: string;
        status?: string;
    };
};
export type Chat = {
    /** Chat JID identifier */
    jid?: string;
    /** Chat display name */
    name?: string;
    /** Timestamp of the last message */
    last_message_time?: string;
    /** Ephemeral message expiration time in seconds (0 = disabled) */
    ephemeral_expiration?: number;
    /** Chat creation timestamp */
    created_at?: string;
    /** Chat last update timestamp */
    updated_at?: string;
};
export type ChatListResponse = {
    code?: string;
    message?: string;
    results?: {
        data?: Chat[];
        pagination?: {
            limit?: number;
            offset?: number;
            total?: number;
        };
    };
};
export type ErrorUnauthorized = {
    /** HTTP Status Code */
    code?: string;
    /** Detail error message */
    message?: string;
    /** additional data */
    results?: object;
};
export type ChatMessage = {
    /** Message ID */
    id?: string;
    /** Chat JID this message belongs to */
    chat_jid?: string;
    /** Sender JID */
    sender_jid?: string;
    /** Message text content */
    content?: string;
    /** Message timestamp */
    timestamp?: string;
    /** Whether this message was sent by the current user */
    is_from_me?: boolean;
    /** Type of media (image, video, audio, document, etc.) */
    media_type?: string | null;
    /** Original filename for media messages */
    filename?: string | null;
    /** Media file URL */
    url?: string | null;
    /** File size in bytes for media messages */
    file_length?: number | null;
    /** Record creation timestamp */
    created_at?: string;
    /** Record last update timestamp */
    updated_at?: string;
};
export type ChatMessagesResponse = {
    code?: string;
    message?: string;
    results?: {
        data?: ChatMessage[];
        pagination?: {
            limit?: number;
            offset?: number;
            total?: number;
        };
        chat_info?: Chat;
    };
};
export type ErrorNotFound = {
    /** HTTP Status Code */
    code?: string;
    /** Detail error message */
    message?: string;
    /** additional data */
    results?: object;
};
export type LabelChatResponse = {
    code?: string;
    message?: string;
    results?: {
        status?: string;
        message?: string;
        chat_jid?: string;
        label_id?: string;
        labeled?: boolean;
    };
};
export type PinChatResponse = {
    code?: string;
    message?: string;
    results?: {
        status?: string;
        message?: string;
        chat_jid?: string;
        pinned?: boolean;
    };
};
export type GroupInfoResponse = {
    status?: number;
    code?: string;
    message?: string;
    /** Group information object (structure may vary) */
    results?: {
        [key: string]: unknown;
    };
};
export type CreateGroupResponse = {
    code?: string;
    message?: string;
    results?: {
        group_id?: string;
    };
};
export type ManageParticipantRequest = {
    group_id?: string;
    participants?: string[];
};
export type ManageParticipantResponse = {
    code?: string;
    message?: string;
    results?: {
        participant?: string;
        status?: string;
        message?: string;
    }[];
};
export type GroupInfoFromLinkResponse = {
    code?: string;
    message?: string;
    results?: {
        /** The group ID */
        group_id?: string;
        /** The group name */
        name?: string;
        /** The group topic/description */
        topic?: string;
        /** When the group was created */
        created_at?: string;
        /** Number of participants in the group */
        participant_count?: number;
        /** Whether the group is locked (only admins can modify group info) */
        is_locked?: boolean;
        /** Whether the group is in announce mode (only admins can send messages) */
        is_announce?: boolean;
        /** Whether the group has disappearing messages enabled */
        is_ephemeral?: boolean;
        /** Additional description of the group */
        description?: string;
    };
};
export type GroupParticipantRequestListResponse = {
    code?: string;
    message?: string;
    results?: {
        data?: {
            jid?: string;
            requested_at?: string;
        }[];
    };
};
export type SetGroupPhotoResponse = {
    code?: string;
    message?: string;
    results?: {
        /** The ID of the uploaded picture, or 'remove' if photo was removed */
        picture_id?: string;
        message?: string;
    };
};
export type GetGroupInviteLinkResponse = {
    code?: string;
    message?: string;
    results?: {
        /** The group invite link */
        invite_link?: string;
        /** The group ID */
        group_id?: string;
    };
};
/**
 * Login to whatsapp server
 */
export function appLogin(opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: LoginResponse;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/app/login", {
        ...opts
    });
}
/**
 * Login with pairing code
 */
export function appLoginWithCode({ phone }: {
    phone?: string;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: LoginWithCodeResponse;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/app/login-with-code${QS.query(QS.explode({
        phone
    }))}`, {
        ...opts
    });
}
/**
 * Remove database and logout
 */
export function appLogout(opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/app/logout", {
        ...opts
    });
}
/**
 * Reconnecting to whatsapp server
 */
export function appReconnect(opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/app/reconnect", {
        ...opts
    });
}
/**
 * Get list connected devices
 */
export function appDevices(opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: DeviceResponse;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/app/devices", {
        ...opts
    });
}
/**
 * User Info
 */
export function userInfo({ phone }: {
    phone?: string;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: UserInfoResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/user/info${QS.query(QS.explode({
        phone
    }))}`, {
        ...opts
    });
}
/**
 * User Avatar
 */
export function userAvatar({ phone, isPreview, isCommunity }: {
    phone?: string;
    isPreview?: boolean;
    isCommunity?: boolean;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: UserAvatarResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/user/avatar${QS.query(QS.explode({
        phone,
        is_preview: isPreview,
        is_community: isCommunity
    }))}`, {
        ...opts
    });
}
/**
 * User Change Avatar
 */
export function userChangeAvatar({ body }: {
    body?: {
        /** Avatar to send */
        avatar?: Blob;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/user/avatar", oazapfts.multipart({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * User Change Push Name
 */
export function userChangePushName({ body }: {
    body?: {
        /** The new display name to set */
        push_name: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/user/pushname", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * User My Privacy Setting
 */
export function userMyPrivacy(opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: UserPrivacyResponse;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/user/my/privacy", {
        ...opts
    });
}
/**
 * User My List Groups
 */
export function userMyGroups(opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: UserGroupResponse;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/user/my/groups", {
        ...opts
    });
}
/**
 * User My List Groups
 */
export function userMyNewsletter(opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: NewsletterResponse;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/user/my/newsletters", {
        ...opts
    });
}
/**
 * Get list of user contacts
 */
export function userMyContacts(opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: MyListContactsResponse;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/user/my/contacts", {
        ...opts
    });
}
/**
 * Check if user is on WhatsApp
 */
export function userCheck({ phone }: {
    phone?: string;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: UserCheckResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/user/check${QS.query(QS.explode({
        phone
    }))}`, {
        ...opts
    });
}
/**
 * Get Business Profile Information
 */
export function userBusinessProfile({ phone }: {
    phone: string;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: BusinessProfileResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/user/business-profile${QS.query(QS.explode({
        phone
    }))}`, {
        ...opts
    });
}
/**
 * Send Message
 */
export function sendMessage({ body }: {
    body?: {
        /** Phone number with country code */
        phone?: string;
        /** Message to send */
        message?: string;
        /** Message ID that you want reply */
        reply_message_id?: string;
        /** Whether this is a forwarded message */
        is_forwarded?: boolean;
        /** Disappearing message duration in seconds (optional) */
        duration?: number;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/message", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send Image
 */
export function sendImage({ body }: {
    body?: {
        /** Phone number with country code */
        phone?: string;
        /** Caption to send */
        caption?: string;
        /** View once */
        view_once?: boolean;
        /** Image to send */
        image?: Blob;
        /** Image URL to send */
        image_url?: string;
        /** Compress image */
        compress?: boolean;
        /** Disappearing message duration in seconds (optional) */
        duration?: number;
        /** Whether this is a forwarded message */
        is_forwarded?: boolean;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/image", oazapfts.multipart({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send Audio
 */
export function sendAudio({ body }: {
    body?: {
        /** Phone number with country code */
        phone?: string;
        /** Audio to send */
        audio?: Blob;
        /** Audio URL to send */
        audio_url?: string;
        /** Whether this is a forwarded message */
        is_forwarded?: boolean;
        /** Disappearing message duration in seconds (optional) */
        duration?: number;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/audio", oazapfts.multipart({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send File
 */
export function sendFile({ body }: {
    body?: {
        /** Phone number with country code */
        phone?: string;
        /** Caption to send */
        caption?: string;
        /** File to send */
        file?: Blob;
        /** Whether this is a forwarded message */
        is_forwarded?: boolean;
        /** Disappearing message duration in seconds (optional) */
        duration?: number;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/file", oazapfts.multipart({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send Video
 */
export function sendVideo({ body }: {
    body?: {
        /** Phone number with country code */
        phone?: string;
        /** Caption to send */
        caption?: string;
        /** View once */
        view_once?: boolean;
        /** Video to send */
        video?: Blob;
        /** Video URL to send */
        video_url?: string;
        /** Compress video */
        compress?: boolean;
        /** Disappearing message duration in seconds (optional) */
        duration?: number;
        /** Whether this is a forwarded message */
        is_forwarded?: boolean;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/video", oazapfts.multipart({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send Contact
 */
export function sendContact({ body }: {
    body?: {
        /** Phone number with country code */
        phone?: string;
        /** Contact name */
        contact_name?: string;
        /** Contact phone number */
        contact_phone?: string;
        /** Whether this is a forwarded message */
        is_forwarded?: boolean;
        /** Disappearing message duration in seconds (optional) */
        duration?: number;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/contact", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send Link
 */
export function sendLink({ body }: {
    body?: {
        /** Phone number with country code */
        phone?: string;
        /** Link to send */
        link?: string;
        /** Caption to send */
        caption?: string;
        /** Whether this is a forwarded message */
        is_forwarded?: boolean;
        /** Disappearing message duration in seconds (optional) */
        duration?: number;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/link", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send Location
 */
export function sendLocation({ body }: {
    body?: {
        /** Phone number with country code */
        phone?: string;
        /** Latitude coordinate */
        latitude?: string;
        /** Longitude coordinate */
        longitude?: string;
        /** Whether this is a forwarded message */
        is_forwarded?: boolean;
        /** Disappearing message duration in seconds (optional) */
        duration?: number;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/location", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send Poll / Vote
 */
export function sendPoll({ body }: {
    body: {
        /** The WhatsApp phone number to send the poll to, including the '@s.whatsapp.net' suffix. */
        phone: string;
        /** The question for the poll. */
        question: string;
        /** The options for the poll. */
        options: string[];
        /** The maximum number of answers allowed for the poll. */
        max_answer: number;
        /** Disappearing message duration in seconds (optional) */
        duration?: number;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/poll", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send presence status
 */
export function sendPresence({ body }: {
    body: {
        /** The presence type to send */
        "type": "available" | "unavailable";
        /** Whether this is a forwarded message */
        is_forwarded?: boolean;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/presence", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send chat presence (typing indicator)
 */
export function sendChatPresence({ body }: {
    body: {
        /** Phone number with country code */
        phone: string;
        /** Action to perform - "start" to begin typing indicator, "stop" to end typing indicator */
        action: "start" | "stop";
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/send/chat-presence", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Revoke Message
 */
export function revokeMessage({ messageId, body }: {
    messageId: string;
    body?: {
        /** Phone number with country code */
        phone?: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/message/${encodeURIComponent(messageId)}/revoke`, oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Delete Message
 */
export function deleteMessage({ messageId, body }: {
    messageId: string;
    body?: {
        /** Phone number with country code */
        phone?: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/message/${encodeURIComponent(messageId)}/delete`, oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Send reaction to message
 */
export function reactMessage({ messageId, body }: {
    messageId: string;
    body?: {
        /** Phone number with country code */
        phone?: string;
        /** Emoji to react */
        emoji?: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/message/${encodeURIComponent(messageId)}/reaction`, oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Edit message by message ID before 15 minutes
 */
export function updateMessage({ messageId, body }: {
    messageId: string;
    body?: {
        /** Phone number with country code */
        phone: string;
        /** New message to send */
        message: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/message/${encodeURIComponent(messageId)}/update`, oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Mark as read message
 */
export function readMessage({ messageId, body }: {
    messageId: string;
    body?: {
        /** Phone number with country code */
        phone: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SendResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/message/${encodeURIComponent(messageId)}/read`, oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Star message
 */
export function starMessage({ messageId, body }: {
    messageId: string;
    body?: {
        /** Phone number with country code */
        phone: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/message/${encodeURIComponent(messageId)}/star`, oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Unstar message
 */
export function unstarMessage({ messageId, body }: {
    messageId: string;
    body?: {
        /** Phone number with country code */
        phone: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/message/${encodeURIComponent(messageId)}/unstar`, oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Get list of chats
 */
export function listChats({ limit, offset, search, hasMedia }: {
    limit?: number;
    offset?: number;
    search?: string;
    hasMedia?: boolean;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: ChatListResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 401;
        data: ErrorUnauthorized;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/chats${QS.query(QS.explode({
        limit,
        offset,
        search,
        has_media: hasMedia
    }))}`, {
        ...opts
    });
}
/**
 * Get messages from a specific chat
 */
export function getChatMessages({ chatJid, limit, offset, startTime, endTime, mediaOnly, isFromMe, search }: {
    chatJid: string;
    limit?: number;
    offset?: number;
    startTime?: string;
    endTime?: string;
    mediaOnly?: boolean;
    isFromMe?: boolean;
    search?: string;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: ChatMessagesResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 401;
        data: ErrorUnauthorized;
    } | {
        status: 404;
        data: ErrorNotFound;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/chat/${encodeURIComponent(chatJid)}/messages${QS.query(QS.explode({
        limit,
        offset,
        start_time: startTime,
        end_time: endTime,
        media_only: mediaOnly,
        is_from_me: isFromMe,
        search
    }))}`, {
        ...opts
    });
}
/**
 * Label or unlabel a chat
 */
export function labelChat({ chatJid, body }: {
    chatJid: string;
    body?: {
        /** Unique identifier for the label */
        label_id: string;
        /** Display name for the label */
        label_name: string;
        /** Whether to apply (true) or remove (false) the label */
        labeled: boolean;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: LabelChatResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 401;
        data: ErrorUnauthorized;
    } | {
        status: 404;
        data: ErrorNotFound;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/chat/${encodeURIComponent(chatJid)}/label`, oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Pin or unpin a chat
 */
export function pinChat({ chatJid, body }: {
    chatJid: string;
    body?: {
        /** Whether to pin (true) or unpin (false) the chat */
        pinned: boolean;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: PinChatResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 401;
        data: ErrorUnauthorized;
    } | {
        status: 404;
        data: ErrorNotFound;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/chat/${encodeURIComponent(chatJid)}/pin`, oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Group Info
 */
export function groupInfo({ groupId }: {
    groupId?: string;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GroupInfoResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/group/info${QS.query(QS.explode({
        group_id: groupId
    }))}`, {
        ...opts
    });
}
/**
 * Create group and add participant
 */
export function createGroup({ body }: {
    body?: {
        title?: string;
        participants?: string[];
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: CreateGroupResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Adding more participants to group
 */
export function addParticipantToGroup({ manageParticipantRequest }: {
    manageParticipantRequest?: ManageParticipantRequest;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: ManageParticipantResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/participants", oazapfts.json({
        ...opts,
        method: "POST",
        body: manageParticipantRequest
    }));
}
/**
 * Remove participants from group
 */
export function removeParticipantFromGroup({ manageParticipantRequest }: {
    manageParticipantRequest?: ManageParticipantRequest;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: ManageParticipantResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/participants/remove", oazapfts.json({
        ...opts,
        method: "POST",
        body: manageParticipantRequest
    }));
}
/**
 * Promote participants to admin
 */
export function promoteParticipantToAdmin({ manageParticipantRequest }: {
    manageParticipantRequest?: ManageParticipantRequest;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: ManageParticipantResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/participants/promote", oazapfts.json({
        ...opts,
        method: "POST",
        body: manageParticipantRequest
    }));
}
/**
 * Demote participants to member
 */
export function demoteParticipantToMember({ manageParticipantRequest }: {
    manageParticipantRequest?: ManageParticipantRequest;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: ManageParticipantResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/participants/demote", oazapfts.json({
        ...opts,
        method: "POST",
        body: manageParticipantRequest
    }));
}
/**
 * Join group with link
 */
export function joinGroupWithLink({ body }: {
    body?: {
        link?: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/join-with-link", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Get group information from invitation link
 */
export function getGroupInfoFromLink({ link }: {
    link: string;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GroupInfoFromLinkResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/group/info-from-link${QS.query(QS.explode({
        link
    }))}`, {
        ...opts
    });
}
/**
 * Get list of participant requests to join group
 */
export function getGroupParticipantRequests({ groupId }: {
    groupId: string;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GroupParticipantRequestListResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/group/participant-requests${QS.query(QS.explode({
        group_id: groupId
    }))}`, {
        ...opts
    });
}
/**
 * Approve participant request to join group
 */
export function approveGroupParticipantRequest({ body }: {
    body?: {
        /** The group ID */
        group_id: string;
        /** Array of participant WhatsApp IDs to approve */
        participants: string[];
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/participant-requests/approve", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Reject participant request to join group
 */
export function rejectGroupParticipantRequest({ body }: {
    body?: {
        /** The group ID */
        group_id: string;
        /** Array of participant WhatsApp IDs to reject */
        participants: string[];
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/participant-requests/reject", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Leave group
 */
export function leaveGroup({ body }: {
    body?: {
        group_id?: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/leave", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Set group photo
 */
export function setGroupPhoto({ body }: {
    body?: {
        /** The group ID */
        group_id: string;
        /** Group photo to upload (JPEG format recommended). Leave empty to remove photo. */
        photo?: Blob;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: SetGroupPhotoResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/photo", oazapfts.multipart({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Set group name
 */
export function setGroupName({ body }: {
    body?: {
        /** The group ID */
        group_id: string;
        /** The new group name (max 25 characters) */
        name: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/name", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Set group locked status
 */
export function setGroupLocked({ body }: {
    body?: {
        /** The group ID */
        group_id: string;
        /** Whether to lock the group (true) or unlock it (false) */
        locked: boolean;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/locked", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Set group announce mode
 */
export function setGroupAnnounce({ body }: {
    body?: {
        /** The group ID */
        group_id: string;
        /** Whether to enable announce mode (true) or disable it (false) */
        announce: boolean;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/announce", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Set group topic
 */
export function setGroupTopic({ body }: {
    body?: {
        /** The group ID */
        group_id: string;
        /** The group topic/description. Leave empty to remove the topic. */
        topic?: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/group/topic", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}
/**
 * Group Invite Link
 */
export function groupInviteLink({ groupId, reset }: {
    groupId: string;
    reset?: boolean;
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GetGroupInviteLinkResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>(`/group/invite-link${QS.query(QS.explode({
        group_id: groupId,
        reset
    }))}`, {
        ...opts
    });
}
/**
 * Unfollow newsletter
 */
export function unfollowNewsletter({ body }: {
    body?: {
        newsletter_id?: string;
    };
}, opts?: Oazapfts.RequestOpts) {
    return oazapfts.fetchJson<{
        status: 200;
        data: GenericResponse;
    } | {
        status: 400;
        data: ErrorBadRequest;
    } | {
        status: 500;
        data: ErrorInternalServer;
    }>("/newsletter/unfollow", oazapfts.json({
        ...opts,
        method: "POST",
        body
    }));
}

