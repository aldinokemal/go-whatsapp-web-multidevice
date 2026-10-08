package config

// Full history sync on pairing (opt-in). By default GOWA pairs as a Chrome companion and the phone
// sends only the recent history bootstrap. With WhatsappFullHistorySync the companion pairs as
// Desktop and asks the phone for the full history (same as WhatsApp Desktop / Baileys
// syncFullHistory). Only affects NEW pairings: an already paired device keeps what it got.
var (
	WhatsappFullHistorySync          = false
	WhatsappFullHistoryDays   uint32 = 3650  // FullSyncDaysLimit sent to the phone
	WhatsappFullHistorySizeMB uint32 = 10240 // FullSyncSizeMbLimit sent to the phone
)
