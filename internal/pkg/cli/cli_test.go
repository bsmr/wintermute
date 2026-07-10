package cli

import (
	"context"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string // substring; empty means no error
		wantOut string // substring expected in stdout
	}{
		{name: "no args prints usage", args: nil, wantOut: "usage: wm"},
		{name: "known command is stubbed", args: []string{"build"}, wantErr: "not implemented"},
		{name: "unknown command errors", args: []string{"frobnicate"}, wantErr: "unknown command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut strings.Builder
			err := Run(context.Background(), tt.args, strings.NewReader(""), &out, &errOut)

			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("got err %v, want substring %q", err, tt.wantErr)
			}
			if tt.wantOut != "" && !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("stdout = %q, want substring %q", out.String(), tt.wantOut)
			}
		})
	}
}
