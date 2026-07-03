package validations

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
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

	tests := []struct {
		name    string
		mutate  func(r *types.WebAuthnResponse) *types.WebAuthnResponse
		wantErr string // expected error substring; empty means no error
	}{
		{
			name:   "valid PublicKeyCredential.toJSON payload",
			mutate: func(r *types.WebAuthnResponse) *types.WebAuthnResponse { return r },
		},
		{
			name:    "nil payload",
			mutate:  func(*types.WebAuthnResponse) *types.WebAuthnResponse { return nil },
			wantErr: "assertion payload is required",
		},
		{
			name:    "missing id",
			mutate:  func(r *types.WebAuthnResponse) *types.WebAuthnResponse { r.ID = ""; return r },
			wantErr: "id: cannot be blank",
		},
		{
			name:    "missing rawId",
			mutate:  func(r *types.WebAuthnResponse) *types.WebAuthnResponse { r.RawID = nil; return r },
			wantErr: "rawId: cannot be blank",
		},
		{
			name:    "wrong type",
			mutate:  func(r *types.WebAuthnResponse) *types.WebAuthnResponse { r.Type = "password"; return r },
			wantErr: "type: must be a valid value",
		},
		{
			name:    "missing signature",
			mutate:  func(r *types.WebAuthnResponse) *types.WebAuthnResponse { r.Response.Signature = nil; return r },
			wantErr: "response.signature: cannot be blank",
		},
		{
			name:    "missing authenticatorData",
			mutate:  func(r *types.WebAuthnResponse) *types.WebAuthnResponse { r.Response.AuthenticatorData = nil; return r },
			wantErr: "response.authenticatorData: cannot be blank",
		},
		{
			name:    "missing clientDataJSON",
			mutate:  func(r *types.WebAuthnResponse) *types.WebAuthnResponse { r.Response.ClientDataJSON = nil; return r },
			wantErr: "response.clientDataJSON: cannot be blank",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasskeyResponse(context.Background(), tt.mutate(parse(t, validJSON)))
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}
