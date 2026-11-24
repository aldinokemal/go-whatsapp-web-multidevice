package device

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Shape: a device with no name serialises exactly as upstream's did (no
// "label" key), so single-user mode output is unchanged.
func TestDeviceJSONOmitsEmptyLabel(t *testing.T) {
	d := Device{ID: "d_1", State: DeviceStateLoggedIn, CreatedAt: time.Unix(0, 0).UTC()}
	b, err := json.Marshal(d)
	require.NoError(t, err)
	require.NotContains(t, string(b), "label")

	d.Label = "Work phone"
	b, err = json.Marshal(d)
	require.NoError(t, err)
	require.Contains(t, string(b), `"label":"Work phone"`)
}
