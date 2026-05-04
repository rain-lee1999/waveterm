// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package cmd

import "testing"

func TestRootCommandNameForExecutable(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/tmp/wave", want: "wave"},
		{path: "/tmp/wsh", want: "wsh"},
		{path: "", want: "wsh"},
	}
	for _, tt := range tests {
		if got := rootCommandNameForExecutable(tt.path); got != tt.want {
			t.Fatalf("rootCommandNameForExecutable(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
