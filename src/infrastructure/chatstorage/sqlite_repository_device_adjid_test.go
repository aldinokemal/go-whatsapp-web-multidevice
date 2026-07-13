package chatstorage

import (
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// The devices registry must round-trip the full AD JID (number:NN@s.whatsapp.net) so
// the device manager can key the slot<->companion mapping precisely (issue #760).
func TestDeviceRecordADJIDRoundTrip(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	adJID := "6281777000020:28@s.whatsapp.net"
	nonAD := "6281777000020@s.whatsapp.net"
	if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{
		DeviceID:    "slot-a",
		DisplayName: "Slot A",
		JID:         nonAD,
		ADJID:       adJID,
	}); err != nil {
		t.Fatalf("save device record: %v", err)
	}

	rec, err := repo.GetDeviceRecord("slot-a")
	if err != nil {
		t.Fatalf("get device record: %v", err)
	}
	if rec == nil || rec.ADJID != adJID {
		t.Fatalf("expected AD JID %q from GetDeviceRecord, got %+v", adJID, rec)
	}

	records, err := repo.ListDeviceRecords()
	if err != nil {
		t.Fatalf("list device records: %v", err)
	}
	if len(records) != 1 || records[0].ADJID != adJID {
		t.Fatalf("expected AD JID %q from ListDeviceRecords, got %+v", adJID, records)
	}

	// Updates must persist a cleared AD JID (logout keeps the slot but wipes identity).
	if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{
		DeviceID:    "slot-a",
		DisplayName: "Slot A",
		JID:         "",
		ADJID:       "",
	}); err != nil {
		t.Fatalf("update device record: %v", err)
	}
	rec, err = repo.GetDeviceRecord("slot-a")
	if err != nil {
		t.Fatalf("get updated device record: %v", err)
	}
	if rec == nil || rec.ADJID != "" || rec.JID != "" {
		t.Fatalf("expected cleared JIDs after update, got %+v", rec)
	}
}
