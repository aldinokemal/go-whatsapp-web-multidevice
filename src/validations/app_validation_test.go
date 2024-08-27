package validations

import (
	"context"
	"testing"
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
