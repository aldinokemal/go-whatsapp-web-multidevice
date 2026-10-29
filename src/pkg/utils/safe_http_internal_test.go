package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRejectBlockedConnectChecksEachDialedAddress(t *testing.T) {
	for _, address := range []string{"10.0.0.8:80", "169.254.169.254:80", "[::1]:443", "[::ffff:127.0.0.1]:80"} {
		require.ErrorIs(t, rejectBlockedConnect("tcp", address, nil), errNonPublicAddress, address)
	}
	for _, address := range []string{"1.1.1.1:443", "[2606:4700:4700::1111]:443"} {
		require.NoError(t, rejectBlockedConnect("tcp", address, nil), address)
	}
}
