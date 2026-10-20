package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

// This is a cross-package scenario: the policy revision constrains the visible
// model, and a transfer's ownership generation forces a new connection view.
// Individual malformed inputs and transitions are tested in their own packages.
func TestOfflineScenarioPreservesSharedVersionsAndOwnership(t *testing.T) {
	var output, errorOutput bytes.Buffer
	if code := run(&output, &errorOutput); code != 0 {
		t.Fatalf("lab exited %d: %s %s", code, output.String(), errorOutput.String())
	}
	var result report
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Schema != "metallic.core-lab/v1" || !result.Simulation || result.ProductionReady || result.Error != "" {
		t.Fatalf("incorrect evidence scope: %+v", result)
	}
	if result.PolicyRevision != 2 || result.SessionGeneration != 2 || result.OwnerInstance != "arena:boot2" || result.ContextRevision != 3 || result.Realm != "arena" || !slices.Equal(result.VisiblePlayerIDs, []string{"builder"}) {
		t.Fatalf("wrong final policy/session/view state: %+v", result)
	}
	if len(result.Checks) != 19 {
		t.Fatalf("expected 19 integrated invariant checks, got %d", len(result.Checks))
	}
	for _, c := range result.Checks {
		if !c.Passed {
			t.Errorf("failed invariant: %s", c.Name)
		}
	}
	var repeated bytes.Buffer
	if code := run(&repeated, &errorOutput); code != 0 || !bytes.Equal(output.Bytes(), repeated.Bytes()) {
		t.Fatal("fixed fixture was not repeatable")
	}
}
