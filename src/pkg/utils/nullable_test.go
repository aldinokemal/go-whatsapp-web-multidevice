package utils_test

import (
	"encoding/json"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestNullableUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantSet   bool
		wantValid bool
		wantValue bool
	}{
		{name: "absent key", body: `{}`},
		{name: "explicit null", body: `{"flag": null}`, wantSet: true},
		{name: "true", body: `{"flag": true}`, wantSet: true, wantValid: true, wantValue: true},
		{name: "false", body: `{"flag": false}`, wantSet: true, wantValid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload struct {
				Flag utils.Nullable[bool] `json:"flag"`
			}
			assert.NoError(t, json.Unmarshal([]byte(tt.body), &payload))
			assert.Equal(t, tt.wantSet, payload.Flag.Set)
			assert.Equal(t, tt.wantValid, payload.Flag.Valid)
			assert.Equal(t, tt.wantValue, payload.Flag.Value)

			if tt.wantValid {
				assert.Equal(t, tt.wantValue, *payload.Flag.Ptr())
			} else {
				assert.Nil(t, payload.Flag.Ptr())
			}
		})
	}
}

func TestNullableMarshalJSON(t *testing.T) {
	encoded, err := json.Marshal(utils.Nullable[bool]{Set: true, Valid: true, Value: true})
	assert.NoError(t, err)
	assert.JSONEq(t, `true`, string(encoded))

	encoded, err = json.Marshal(utils.Nullable[bool]{Set: true})
	assert.NoError(t, err)
	assert.JSONEq(t, `null`, string(encoded))
}

func TestNullableRejectsMismatchedType(t *testing.T) {
	var payload struct {
		Flag utils.Nullable[bool] `json:"flag"`
	}
	assert.Error(t, json.Unmarshal([]byte(`{"flag": "yes"}`), &payload))
}
