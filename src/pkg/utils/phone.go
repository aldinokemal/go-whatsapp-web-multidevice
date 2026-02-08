package utils

import "strings"

// NormalizePhoneE164 ensures phone has + prefix for E.164 format.
// Strips WhatsApp JID suffixes (@s.whatsapp.net, @lid, etc.) before formatting.
// Returns empty string if input is empty.
func NormalizePhoneE164(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return phone
	}
	phone = ExtractPhoneFromJID(phone)
	if !strings.HasPrefix(phone, "+") {
		return "+" + phone
	}
	return phone
}

// StripPhonePrefix removes + prefix from phone number.
func StripPhonePrefix(phone string) string {
	return strings.TrimPrefix(strings.TrimSpace(phone), "+")
}

// ExtractPhoneFromJID extracts phone number from JID by stripping the domain part.
// For example, "1234567890@s.whatsapp.net" becomes "1234567890".
func ExtractPhoneFromJID(jid string) string {
	return strings.Split(jid, "@")[0]
}

// CleanPhoneForWhatsApp prepares a phone number for WhatsApp sending.
// Removes + prefix and trims whitespace.
func CleanPhoneForWhatsApp(phone string) string {
	phone = strings.ReplaceAll(phone, "+", "")
	return strings.TrimSpace(phone)
}
