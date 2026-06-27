package media

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
)

func DerivePerJidSrtpKey(callKey []byte, deviceJid string) (core.SrtpKeyingMaterial, error) {
	out, err := hkdf.Key(sha256.New, callKey, nil, deviceJid, 46)
	if err != nil {
		return core.SrtpKeyingMaterial{}, err
	}
	mk := make([]byte, 16)
	ms := make([]byte, 14)
	copy(mk, out[0:16])
	copy(ms, out[16:30])
	return core.SrtpKeyingMaterial{MasterKey: mk, MasterSalt: ms}, nil
}

func GenerateCallKey() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
