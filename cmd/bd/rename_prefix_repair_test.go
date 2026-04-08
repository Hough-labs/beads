//go:build cgo

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

func TestRepairMultiplePrefixes(t *testing.T) {
	tmpDir := t.TempDir()
	testDBPath := filepath.Join(tmpDir, "test.db")

	ctx := context.Background()

	testStore, err := dolt.New(ctx, &dolt.Config{Path: testDBPath})
	if err != nil {
		t.Skipf("skipping: Dolt server not available: %v", err)
	}
	defer testStore.Close()

	// Set globals following TestRenamePrefixCommand pattern
	oldStore := store
	oldActor := actor
	oldDBPath := dbPath
	store = testStore
	actor = "test"
	dbPath = testDBPath
	defer func() {
		store = oldStore
		actor = oldActor
		dbPath = oldDBPath
	}()

	// Set initial prefix
	if err := testStore.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Create issues with multiple prefixes (simulating corruption).
	// CreateIssue accepts explicit IDs without prefix validation,
	// so we can create issues with different prefixes to simulate
	// a corrupted database state.
	testIssues := []types.Issue{
		{ID: "test-1", Title: "Test issue 1", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "test-2", Title: "Test issue 2", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "old-1", Title: "Old issue 1", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "old-2", Title: "Old issue 2", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "another-1", Title: "Another issue 1", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
	}

	for i := range testIssues {
		if err := testStore.CreateIssue(ctx, &testIssues[i], "test"); err != nil {
			t.Fatalf("failed to create issue %s: %v", testIssues[i].ID, err)
		}
	}

	// Verify we have multiple prefixes
	allIssues, err := testStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}

	prefixes := detectPrefixes(allIssues)
	if len(prefixes) != 3 {
		t.Fatalf("expected 3 prefixes, got %d: %v", len(prefixes), prefixes)
	}

	// Test repair — now uses UpdateIssueID (Dolt rename semantics)
	// instead of the old CreateIssue+DeleteIssue approach that caused deadlocks
	if err := repairPrefixes(ctx, testStore, "test", "test", allIssues, prefixes, false); err != nil {
		t.Fatalf("repair failed: %v", err)
	}

	// Verify all issues now have correct prefix
	allIssues, err = testStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues after repair: %v", err)
	}

	prefixes = detectPrefixes(allIssues)
	if len(prefixes) != 1 {
		t.Fatalf("expected 1 prefix after repair, got %d: %v", len(prefixes), prefixes)
	}

	if _, ok := prefixes["test"]; !ok {
		t.Fatalf("expected prefix 'test', got %v", prefixes)
	}

	// Verify the original test-1 and test-2 are unchanged
	for _, id := range []string{"test-1", "test-2"} {
		issue, err := testStore.GetIssue(ctx, id)
		if err != nil {
			t.Fatalf("expected issue %s to exist unchanged: %v", id, err)
		}
		if issue == nil {
			t.Fatalf("expected issue %s to exist", id)
		}
	}

	// Verify total count: 2 original (test-1, test-2) + 3 renamed = 5
	if len(allIssues) != 5 {
		t.Fatalf("expected 5 issues total, got %d", len(allIssues))
	}

	// Count issues with correct prefix
	testPrefixCount := 0
	for _, issue := range allIssues {
		if len(issue.ID) > 5 && issue.ID[:5] == "test-" {
			testPrefixCount++
		}
	}
	if testPrefixCount != 5 {
		t.Fatalf("expected all 5 issues to have 'test-' prefix, got %d", testPrefixCount)
	}

	// Verify old IDs no longer exist
	for _, oldID := range []string{"old-1", "old-2", "another-1"} {
		issue, err := testStore.GetIssue(ctx, oldID)
		if err == nil && issue != nil {
			t.Fatalf("expected old ID %s to no longer exist", oldID)
		}
	}
}

// TestRepairPrefixSubstringCollision is a regression test for a bug where
// RenameDependencyPrefixInTx used an unanchored LIKE ('ba%') so an old prefix
// that was a substring of the target prefix (e.g. 'ba' vs 'bastion') would
// also match the target's own dependency rows and corrupt them
// (`bastion-foo` -> `bastionstion-foo`), producing FK violations.
//
// Setup: target prefix 'bastion' with both bastion-* and ba-* issues, plus
// dependency rows on each side. Repair must succeed and preserve bastion-*.
func TestRepairPrefixSubstringCollision(t *testing.T) {
	tmpDir := t.TempDir()
	testDBPath := filepath.Join(tmpDir, "test.db")

	ctx := context.Background()

	testStore, err := dolt.New(ctx, &dolt.Config{Path: testDBPath})
	if err != nil {
		t.Skipf("skipping: Dolt server not available: %v", err)
	}
	defer testStore.Close()

	oldStore := store
	oldActor := actor
	oldDBPath := dbPath
	store = testStore
	actor = "test"
	dbPath = testDBPath
	defer func() {
		store = oldStore
		actor = oldActor
		dbPath = oldDBPath
	}()

	if err := testStore.SetConfig(ctx, "issue_prefix", "bastion"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// 'bastion-*' is the canonical prefix; 'ba-*' is the corrupting substring.
	testIssues := []types.Issue{
		{ID: "bastion-aaa", Title: "bastion A", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "bastion-bbb", Title: "bastion B", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "ba-x", Title: "ba X", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "ba-y", Title: "ba Y", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
	}
	for i := range testIssues {
		if err := testStore.CreateIssue(ctx, &testIssues[i], "test"); err != nil {
			t.Fatalf("failed to create issue %s: %v", testIssues[i].ID, err)
		}
	}

	// Cross-prefix dependency: ba-x blocks bastion-aaa. After repair, the
	// dependency row referencing 'bastion-aaa' must remain intact (the bug
	// rewrote it to 'bastionstion-aaa' and the FK update later failed).
	deps := []*types.Dependency{
		{IssueID: "bastion-aaa", DependsOnID: "ba-x", Type: types.DepBlocks},
		{IssueID: "ba-y", DependsOnID: "bastion-bbb", Type: types.DepBlocks},
	}
	for _, d := range deps {
		if err := testStore.AddDependency(ctx, d, "test"); err != nil {
			t.Fatalf("failed to add dependency: %v", err)
		}
	}

	allIssues, err := testStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}
	prefixes := detectPrefixes(allIssues)
	if len(prefixes) != 2 {
		t.Fatalf("expected 2 prefixes, got %d: %v", len(prefixes), prefixes)
	}

	// This is the call that previously failed with an FK violation on
	// `bastionstion-aaa` because the unanchored LIKE in
	// RenameDependencyPrefixInTx mangled bastion-* dependency rows.
	if err := repairPrefixes(ctx, testStore, "test", "bastion", allIssues, prefixes, false); err != nil {
		t.Fatalf("repair failed (substring collision regression): %v", err)
	}

	// All issues must now have the bastion- prefix.
	allIssues, err = testStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues after repair: %v", err)
	}
	for _, issue := range allIssues {
		if len(issue.ID) < 8 || issue.ID[:8] != "bastion-" {
			t.Fatalf("issue %s does not have bastion- prefix", issue.ID)
		}
	}

	// The original bastion-aaa / bastion-bbb must survive untouched.
	for _, id := range []string{"bastion-aaa", "bastion-bbb"} {
		issue, err := testStore.GetIssue(ctx, id)
		if err != nil || issue == nil {
			t.Fatalf("expected %s to survive repair, got err=%v issue=%v", id, err, issue)
		}
	}
}

// TestRepairPreservesSemanticSuffix verifies that repair maps
// `<oldPrefix>-<suffix>` to `<targetPrefix>-<suffix>` rather than to an
// opaque content-hash ID. This is what makes identity beads like
// `ba-rig-bastion` survive a rename as `bastion-rig-bastion` (which
// downstream tools like gt doctor expect to find by name).
func TestRepairPreservesSemanticSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	testDBPath := filepath.Join(tmpDir, "test.db")

	ctx := context.Background()

	testStore, err := dolt.New(ctx, &dolt.Config{Path: testDBPath})
	if err != nil {
		t.Skipf("skipping: Dolt server not available: %v", err)
	}
	defer testStore.Close()

	oldStore := store
	oldActor := actor
	oldDBPath := dbPath
	store = testStore
	actor = "test"
	dbPath = testDBPath
	defer func() {
		store = oldStore
		actor = oldActor
		dbPath = oldDBPath
	}()

	if err := testStore.SetConfig(ctx, "issue_prefix", "ba"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// 'ba-' issues need migration to 'bastion-'. Their semantic suffixes
	// (rig-bastion, bastion-witness, ag8) should all be preserved.
	testIssues := []types.Issue{
		{ID: "ba-rig-bastion", Title: "rig identity", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "ba-bastion-witness", Title: "witness patrol", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "ba-ag8", Title: "refinery patrol", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
	}
	for i := range testIssues {
		if err := testStore.CreateIssue(ctx, &testIssues[i], "test"); err != nil {
			t.Fatalf("failed to create issue %s: %v", testIssues[i].ID, err)
		}
	}

	allIssues, err := testStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("failed to search issues: %v", err)
	}
	prefixes := detectPrefixes(allIssues)

	// Force the repair path even with one detected prefix by passing the
	// target prefix that doesn't match (mirrors the real-world setup where
	// rename-prefix bastion --repair is invoked against a 'ba'-only db).
	if err := repairPrefixes(ctx, testStore, "test", "bastion", allIssues, prefixes, false); err != nil {
		t.Fatalf("repair failed: %v", err)
	}

	// Each issue must now have its semantic ID, not a hash.
	expected := []string{"bastion-rig-bastion", "bastion-bastion-witness", "bastion-ag8"}
	for _, want := range expected {
		issue, err := testStore.GetIssue(ctx, want)
		if err != nil || issue == nil {
			t.Fatalf("expected semantic ID %s after repair, got err=%v issue=%v", want, err, issue)
		}
	}
}
