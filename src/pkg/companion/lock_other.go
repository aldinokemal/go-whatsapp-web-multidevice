//go:build !unix

package companion

type fileLock struct{}

func acquireLock(string) (*fileLock, error) { return nil, block("invalid_request") }
func (l *fileLock) Close() error            { return nil }
