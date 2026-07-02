package signaling

import (
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
)

func TestParseRelayFromAck(t *testing.T) {

	addr := []byte{1, 2, 3, 4, 0x0d, 0x98}
	rtt := 7

	ack := &waBinary.Node{
		Tag: "ack",
		Content: []waBinary.Node{
			{
				Tag: "user",
				Content: []waBinary.Node{
					{Tag: "device", Attrs: waBinary.Attrs{"jid": "111:0@lid"}},
					{Tag: "device", Attrs: waBinary.Attrs{"jid": "222:1@lid"}},
				},
			},
			{
				Tag:   "relay",
				Attrs: waBinary.Attrs{"uuid": "abc-uuid", "self_pid": "5", "peer_pid": "9"},
				Content: []waBinary.Node{
					{Tag: "participant", Attrs: waBinary.Attrs{"jid": "333:2@lid"}},
					{Tag: "key", Content: []byte("relaykey123")},
					{Tag: "token", Attrs: waBinary.Attrs{"id": "0"}, Content: []byte{0xAA, 0xBB}},
					{Tag: "auth_token", Attrs: waBinary.Attrs{"id": "1"}, Content: []byte{0xCC, 0xDD}},
					{
						Tag: "te2",
						Attrs: waBinary.Attrs{
							"token_id": "0", "auth_token_id": "1",
							"relay_name": "relay-A", "protocol": "0",
							"relay_id": "2", "c2r_rtt": "7",
						},
						Content: addr,
					},
				},
			},
		},
	}

	res := ParseRelayFromAck(ack)

	if res.UUID != "abc-uuid" {
		t.Errorf("uuid = %q", res.UUID)
	}
	if res.SelfPid == nil || *res.SelfPid != 5 {
		t.Errorf("self_pid = %v", res.SelfPid)
	}
	if res.PeerPid == nil || *res.PeerPid != 9 {
		t.Errorf("peer_pid = %v", res.PeerPid)
	}
	want := []string{"111:0@lid", "222:1@lid", "333:2@lid"}
	if len(res.ParticipantJids) != len(want) {
		t.Fatalf("participants = %v", res.ParticipantJids)
	}
	for i := range want {
		if res.ParticipantJids[i] != want[i] {
			t.Errorf("participant[%d] = %q want %q", i, res.ParticipantJids[i], want[i])
		}
	}
	if len(res.Relays) != 1 {
		t.Fatalf("expected 1 relay, got %d", len(res.Relays))
	}
	ep := res.Relays[0]
	if ep.IP != "1.2.3.4" {
		t.Errorf("ip = %q", ep.IP)
	}
	if ep.Port != 3480 {
		t.Errorf("port = %d", ep.Port)
	}
	if ep.Key != "relaykey123" {
		t.Errorf("key = %q", ep.Key)
	}
	if ep.RelayName != "relay-A" {
		t.Errorf("relay_name = %q", ep.RelayName)
	}
	if ep.RelayID != 2 {
		t.Errorf("relay_id = %d", ep.RelayID)
	}
	if ep.C2RRtt == nil || *ep.C2RRtt != rtt {
		t.Errorf("c2r_rtt = %v", ep.C2RRtt)
	}
	if len(ep.RawToken) != 2 || ep.RawToken[0] != 0xAA {
		t.Errorf("raw token = %v", ep.RawToken)
	}
	if len(ep.RawAuthToken) != 2 || ep.RawAuthToken[0] != 0xCC {
		t.Errorf("raw auth token = %v", ep.RawAuthToken)
	}
}
