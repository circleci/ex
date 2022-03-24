package cloudenv

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestProvider_String(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     string
	}{
		{
			name:     "EC2",
			provider: ProviderEC2,
			want:     "EC2",
		},
		{
			name:     "GCE",
			provider: ProviderGCE,
			want:     "GCE",
		},
		{
			name:     "Unknown provider",
			provider: ProviderUnknown,
			want:     "UNKNOWN",
		},
		{
			name:     "Negative provider",
			provider: -1,
			want:     "UNKNOWN",
		},
		{
			name:     "Out of range provider",
			provider: 3,
			want:     "UNKNOWN",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Check(t, cmp.Equal(tt.provider.String(), tt.want))
		})
	}
}

func TestProvider_UnmarshalText(t *testing.T) {
	tests := []struct {
		name         string
		text         []byte
		wantProvider Provider
		wantError    string
	}{
		{
			name:         "Read EC2",
			text:         []byte("EC2"),
			wantProvider: ProviderEC2,
		},
		{
			name:         "Read GCE",
			text:         []byte("GCE"),
			wantProvider: ProviderGCE,
		},
		{
			name:         "Invalid provider",
			text:         []byte("this is invalid"),
			wantProvider: ProviderUnknown,
			wantError:    `unknown provider: "this is invalid"`,
		},
		{
			name:         "Nil input",
			text:         nil,
			wantProvider: ProviderUnknown,
			wantError:    `unknown provider: ""`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p Provider
			err := p.UnmarshalText(tt.text)
			if tt.wantError != "" {
				assert.Check(t, cmp.ErrorContains(err, tt.wantError))
			} else {
				assert.Check(t, err)
			}
			assert.Check(t, cmp.Equal(p, tt.wantProvider))
		})
	}
}
