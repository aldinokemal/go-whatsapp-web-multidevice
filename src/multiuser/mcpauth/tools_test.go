package mcpauth

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMyDevicesListsOnlyMine(t *testing.T) {
	e := newMCPEnv(t)
	require.NoError(t, e.st.SetOwner(context.Background(), "d_spare", e.alice.ID))
	r := e.rpc(mcpRequest(e.aliceAT, "", toolCall(MyDevicesTool, nil)))
	isErr, text := toolOutcome(t, r)
	require.False(t, isErr, text)
	res := r.body["result"].(map[string]any)
	b, _ := json.Marshal(res["structuredContent"])
	var out struct {
		Total int `json:"total"`
		Items []struct {
			DeviceID string `json:"device_id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(b, &out))
	var ids []string
	for _, it := range out.Items {
		ids = append(ids, it.DeviceID)
	}
	assert.ElementsMatch(t, []string{"d_alice", "d_spare"}, ids)
	assert.Equal(t, len(out.Items), out.Total)
	assert.NotContains(t, r.raw, "d_bob")
}
