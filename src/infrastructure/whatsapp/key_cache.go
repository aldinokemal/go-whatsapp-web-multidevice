package whatsapp

import "go.mau.fi/whatsmeow/store"

func applyKeyCacheStore(device *store.Device, keyCacheStore store.AllSessionSpecificStores) {
	device.Identities = keyCacheStore
	device.Sessions = keyCacheStore
	device.PreKeys = keyCacheStore
	device.SenderKeys = keyCacheStore
	device.MsgSecrets = keyCacheStore
}
