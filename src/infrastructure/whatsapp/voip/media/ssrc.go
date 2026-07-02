package media

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
)

func GenerateSecureSsrc(callID, selfJid string, counter uint32) uint32 {
	salt := make([]byte, 4)
	binary.LittleEndian.PutUint32(salt, counter)

	out, err := hkdf.Key(sha256.New, []byte(callID), salt, selfJid, 4)
	if err != nil {

		panic(err)
	}
	return binary.LittleEndian.Uint32(out)
}
