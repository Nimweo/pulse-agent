package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nimweo/pulse-agent/internal/transport"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", want: 0},
		{name: "generic failure", err: errors.New("failed"), want: 1},
		{
			name: "authentication failure",
			err:  fmt.Errorf("wrapped: %w", transport.ErrAuthentication),
			want: authenticationExitCode,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := exitCode(test.err); got != test.want {
				t.Fatalf("exitCode() = %d, want %d", got, test.want)
			}
		})
	}
}
