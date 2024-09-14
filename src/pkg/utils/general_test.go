package utils_test

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestContainsMention(t *testing.T) {
	type args struct {
		message string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "should success get phone when @ with space",
			args: args{message: "welcome @6289123 ."},
			want: []string{"6289123"},
		},
		{
			name: "should success get phone without suffix space",
			args: args{message: "welcome @6289123."},
			want: []string{"6289123"},
		},
		{
			name: "should success get phone without prefix space",
			args: args{message: "welcome@6289123.@hello:@62891823"},
			want: []string{"6289123", "62891823"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utils.ContainsMention(tt.args.message)
			assert.Equal(t, tt.want, got)
		})
	}
}
