// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

// The closeout command requires exactly one dest arg.
func TestCloseoutCmd_RequiresDest(t *testing.T) {
	cmd := closeoutCmd()
	cmd.SetArgs([]string{})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil {
		t.Error("expected an error with no dest argument")
	}
}

// Help must make the safety model explicit: dry-run default + confirm + never
// silent delete + retention-elapsed gate — a reviewer must see these up front.
func TestCloseoutCmd_HelpStatesSafetyModel(t *testing.T) {
	long := closeoutCmd().Long
	for _, want := range []string{"DRY RUN", "--confirm", "never silent delete", "ELAPSED", "provenance record"} {
		if !strings.Contains(long, want) {
			t.Errorf("closeout help missing the safety phrase %q", want)
		}
	}
}

// The default has confirm=false (dry run) — destruction is opt-in, not the default.
func TestCloseoutCmd_DefaultsToDryRun(t *testing.T) {
	cmd := closeoutCmd()
	f := cmd.Flags().Lookup("confirm")
	if f == nil {
		t.Fatal("missing --confirm flag")
	}
	if f.DefValue != "false" {
		t.Errorf("--confirm default = %q, want false (dry run by default)", f.DefValue)
	}
}
