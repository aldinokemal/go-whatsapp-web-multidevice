package validations

import (
	"context"
	"encoding/json"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestValidateLoginWithCode(t *testing.T) {
	type args struct {
		phoneNumber string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name:    "Phone with +",
			args:    args{phoneNumber: "+6281234567890"},
			wantErr: false,
		},
		{
			name:    "Phone without +",
			args:    args{phoneNumber: "621234567890"},
			wantErr: false,
		},
		{
			name:    "Phone with 0",
			args:    args{phoneNumber: "081234567890"},
			wantErr: false,
		},
		{
			name:    "Phone contains alphabet",
			args:    args{phoneNumber: "+6281234567890a"},
			wantErr: true,
		},
		{
			name:    "Empty phone number",
			args:    args{phoneNumber: ""},
			wantErr: true,
		},
		{
			name:    "Phone with special characters",
			args:    args{phoneNumber: "+6281234567890!@#"},
			wantErr: true,
		},
		{
			name:    "Extremely long phone number",
			args:    args{phoneNumber: "+62812345678901234567890"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateLoginWithCode(context.Background(), tt.args.phoneNumber); (err != nil) != tt.wantErr {
				t.Errorf("ValidateLoginWithCode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePasskeyResponse(t *testing.T) {
	// Sample PublicKeyCredential.toJSON() output. The binary fields are unpadded
	// base64url encodings of obviously fake placeholders (e.g. "fake-signature")
	// so secret scanners don't flag them as high-entropy credentials.
	validJSON := `{
		"id": "ZmFrZS1jcmVkZW50aWFsLWlk",
		"rawId": "ZmFrZS1jcmVkZW50aWFsLWlk",
		"type": "public-key",
		"response": {
			"clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0In0",
			"authenticatorData": "ZmFrZS1hdXRoZW50aWNhdG9yLWRhdGE",
			"signature": "ZmFrZS1zaWduYXR1cmU",
			"userHandle": null
		}
	}`

	parse := func(t *testing.T, raw string) *types.WebAuthnResponse {
		var resp types.WebAuthnResponse
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("failed to unmarshal sample assertion: %v", err)
		}
		return &resp
	}

	t.Run("valid PublicKeyCredential.toJSON payload", func(t *testing.T) {
		if err := ValidatePasskeyResponse(context.Background(), parse(t, validJSON)); err != nil {
			t.Errorf("ValidatePasskeyResponse() error = %v, wantErr false", err)
		}
	})

	t.Run("nil payload", func(t *testing.T) {
		if err := ValidatePasskeyResponse(context.Background(), nil); err == nil {
			t.Error("ValidatePasskeyResponse() error = nil, wantErr true")
		}
	})

	t.Run("missing id", func(t *testing.T) {
		resp := parse(t, validJSON)
		resp.ID = ""
		if err := ValidatePasskeyResponse(context.Background(), resp); err == nil {
			t.Error("ValidatePasskeyResponse() error = nil, wantErr true")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		resp := parse(t, validJSON)
		resp.Type = "password"
		if err := ValidatePasskeyResponse(context.Background(), resp); err == nil {
			t.Error("ValidatePasskeyResponse() error = nil, wantErr true")
		}
	})

	t.Run("missing signature", func(t *testing.T) {
		resp := parse(t, validJSON)
		resp.Response.Signature = nil
		if err := ValidatePasskeyResponse(context.Background(), resp); err == nil {
			t.Error("ValidatePasskeyResponse() error = nil, wantErr true")
		}
	})

	t.Run("missing clientDataJSON", func(t *testing.T) {
		resp := parse(t, validJSON)
		resp.Response.ClientDataJSON = nil
		if err := ValidatePasskeyResponse(context.Background(), resp); err == nil {
			t.Error("ValidatePasskeyResponse() error = nil, wantErr true")
		}
	})
}
