package chatstorage

// WTF: PurgeResult counts what one retention purge removed.
type PurgeResult struct {
	Messages int64
	Chats    int64
}
