package passkey

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

// softAuth is a minimal software authenticator (ES256, "none" attestation) so
// the tests can run real WebAuthn ceremonies against go-webauthn.
type softAuth struct {
	key    *ecdsa.PrivateKey
	credID []byte
	userID []byte // the user handle it stored at registration
	rpID   string
	origin string
	count  uint32
	flags  byte // flags used for every response; default UP|UV|AT handled per call
}

const (
	flagUP = 0x01
	flagUV = 0x04
	flagAT = 0x40
)

var b64 = base64.RawURLEncoding

func newSoftAuth(t *testing.T) *softAuth {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &softAuth{key: k, credID: id, rpID: "wtf.onrender.com", origin: "https://wtf.onrender.com", flags: flagUP | flagUV}
}

func challengeOf(t *testing.T, opts []byte) string {
	t.Helper()
	var o struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			User      struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal(opts, &o))
	require.NotEmpty(t, o.PublicKey.Challenge)
	return o.PublicKey.Challenge
}

func userIDOf(t *testing.T, opts []byte) []byte {
	t.Helper()
	var o struct {
		PublicKey struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal(opts, &o))
	id, err := b64.DecodeString(o.PublicKey.User.ID)
	require.NoError(t, err)
	return id
}

func (a *softAuth) clientData(typ, challenge string) []byte {
	js, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": a.origin, "crossOrigin": false})
	return js
}

func (a *softAuth) authData(flags byte, attested []byte) []byte {
	h := sha256.Sum256([]byte(a.rpID))
	out := append([]byte{}, h[:]...)
	out = append(out, flags)
	var c [4]byte
	binary.BigEndian.PutUint32(c[:], a.count)
	out = append(out, c[:]...)
	return append(out, attested...)
}

// register answers navigator.credentials.create() for the given options.
func (a *softAuth) register(t *testing.T, opts []byte) []byte {
	t.Helper()
	a.userID = userIDOf(t, opts)
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: a.key.X.FillBytes(make([]byte, 32)), -3: a.key.Y.FillBytes(make([]byte, 32))})
	require.NoError(t, err)
	att := make([]byte, 16) // zero AAGUID
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(a.credID)))
	att = append(att, l[:]...)
	att = append(att, a.credID...)
	att = append(att, cose...)
	attObj, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": a.authData(a.flags|flagAT, att)})
	require.NoError(t, err)
	body, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(a.clientData("webauthn.create", challengeOf(t, opts))),
			"attestationObject": b64.EncodeToString(attObj),
		},
		"clientExtensionResults": map[string]any{},
	})
	return body
}

// login answers navigator.credentials.get() for the given options, using the
// stored user handle (override with handle to forge one).
func (a *softAuth) login(t *testing.T, opts []byte, handle []byte) []byte {
	t.Helper()
	if handle == nil {
		handle = a.userID
	}
	cdj := a.clientData("webauthn.get", challengeOf(t, opts))
	ad := a.authData(a.flags, nil)
	sum := sha256.Sum256(cdj)
	digest := sha256.Sum256(append(append([]byte{}, ad...), sum[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	require.NoError(t, err)
	body, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(cdj),
			"authenticatorData": b64.EncodeToString(ad),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(handle),
		},
		"clientExtensionResults": map[string]any{},
	})
	return body
}
