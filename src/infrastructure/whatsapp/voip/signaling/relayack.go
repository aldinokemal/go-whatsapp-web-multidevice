package signaling

import (
	"encoding/base64"
	"sort"
	"strconv"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/wanode"

	waBinary "go.mau.fi/whatsmeow/binary"
)

type ParsedRelayAck struct {
	Relays          []core.RelayEndpoint
	ParticipantJids []string
	UUID            string
	SelfPid         *int
	PeerPid         *int
	HbhKey          []byte
}

func ParseRelayFromAck(ackNode *waBinary.Node) ParsedRelayAck {
	res := ParsedRelayAck{}
	participantSeen := map[string]bool{}

	addParticipant := func(jid string) {
		if jid != "" && !participantSeen[jid] {
			participantSeen[jid] = true
			res.ParticipantJids = append(res.ParticipantJids, jid)
		}
	}

	for _, child := range wanode.NodeChildren(ackNode) {
		child := child

		if child.Tag == "user" {
			for _, deviceNode := range wanode.NodeChildren(&child) {
				if deviceNode.Tag == "device" && wanode.HasAttr(deviceNode.Attrs, "jid") {
					addParticipant(wanode.AttrString(deviceNode.Attrs, "jid"))
				}
			}
		}

		if child.Tag != "relay" {
			continue
		}

		res.UUID = wanode.AttrString(child.Attrs, "uuid")
		if wanode.HasAttr(child.Attrs, "self_pid") {
			v := wanode.AttrInt(child.Attrs, "self_pid", 0)
			res.SelfPid = &v
		}
		if wanode.HasAttr(child.Attrs, "peer_pid") {
			v := wanode.AttrInt(child.Attrs, "peer_pid", 0)
			res.PeerPid = &v
		}

		relayContent := wanode.NodeChildren(&child)

		for _, rc := range relayContent {
			if rc.Tag == "participant" && wanode.HasAttr(rc.Attrs, "jid") {
				addParticipant(wanode.AttrString(rc.Attrs, "jid"))
			}
		}

		var relayKey string
		tokens := map[string]string{}
		authTokens := map[string]string{}
		rawTokens := map[string][]byte{}
		rawAuthTokens := map[string][]byte{}

		for _, rc := range relayContent {
			rc := rc
			switch rc.Tag {
			case "key":
				if b := wanode.NodeBytes(&rc); b != nil {
					relayKey = string(b)
				}
			case "hbh_key":
				if b := wanode.NodeBytes(&rc); b != nil {
					switch {
					case len(b) == 30:
						res.HbhKey = b
					case len(b) > 30:
						if decoded, err := base64.StdEncoding.DecodeString(string(b)); err == nil && len(decoded) == 30 {
							res.HbhKey = decoded
						}
					}
				}
			case "token":
				if b := wanode.NodeBytes(&rc); b != nil {
					id := attrStringOr(rc.Attrs, "id", "0")
					tokens[id] = base64.StdEncoding.EncodeToString(b)
					rawTokens[id] = b
				}
			case "auth_token":
				if b := wanode.NodeBytes(&rc); b != nil {
					id := attrStringOr(rc.Attrs, "id", "0")
					authTokens[id] = base64.StdEncoding.EncodeToString(b)
					rawAuthTokens[id] = b
				}
			}
		}

		for _, rc := range relayContent {
			rc := rc
			if rc.Tag != "te2" {
				continue
			}
			addrBytes := wanode.NodeBytes(&rc)
			if len(addrBytes) < 6 {
				continue
			}

			tokenID := attrStringOr(rc.Attrs, "token_id", "0")
			authTokenID := wanode.AttrString(rc.Attrs, "auth_token_id")
			relayName := wanode.AttrString(rc.Attrs, "relay_name")
			protocol := wanode.AttrInt(rc.Attrs, "protocol", 0)

			ep := core.RelayEndpoint{
				Token:        tokens[tokenID],
				RawToken:     rawTokens[tokenID],
				Key:          relayKey,
				RelayID:      wanode.AttrInt(rc.Attrs, "relay_id", 0),
				Protocol:     protocol,
				RelayName:    relayName,
				AddressBytes: append([]byte(nil), addrBytes...),
			}
			if authTokenID != "" {
				ep.AuthToken = authTokens[authTokenID]
				ep.RawAuthToken = rawAuthTokens[authTokenID]
				ep.AuthTokenID = authTokenID
			} else {
				ep.AuthTokenID = tokenID
			}
			if wanode.HasAttr(rc.Attrs, "c2r_rtt") {
				v := wanode.AttrInt(rc.Attrs, "c2r_rtt", 0)
				ep.C2RRtt = &v
			}

			if len(addrBytes) == 6 {
				ep.IP = ipv4String(addrBytes[:4])
				ep.Port = int(addrBytes[4])<<8 | int(addrBytes[5])
				res.Relays = append(res.Relays, ep)
			}
		}
	}

	sortRelaysByRtt(res.Relays)
	return res
}

func attrStringOr(attrs waBinary.Attrs, key, fallback string) string {
	if s := wanode.AttrString(attrs, key); s != "" {
		return s
	}
	return fallback
}

func ipv4String(b []byte) string {
	return strconv.Itoa(int(b[0])) + "." + strconv.Itoa(int(b[1])) + "." +
		strconv.Itoa(int(b[2])) + "." + strconv.Itoa(int(b[3]))
}

func sortRelaysByRtt(relays []core.RelayEndpoint) {
	sort.SliceStable(relays, func(i, j int) bool {
		ri, rj := relays[i].C2RRtt, relays[j].C2RRtt
		switch {
		case ri == nil && rj == nil:
			return false
		case ri == nil:
			return false
		case rj == nil:
			return true
		default:
			return *ri < *rj
		}
	})
}
