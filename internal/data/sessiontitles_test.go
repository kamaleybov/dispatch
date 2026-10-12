package data

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeWorkspaceMetadata(t *testing.T, root, sessionID, content string) string {
	t.Helper()
	dir := filepath.Join(root, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "workspace.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScanUserTitles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DISPATCH_SESSION_STATE", root)

	writeWorkspaceMetadata(t, root, "renamed-session", "name: 'Fix: quoted title'\nuser_named: true\n")
	writeWorkspaceMetadata(t, root, "automatic-session", "name: generated title\nuser_named: false\n")
	writeWorkspaceMetadata(t, root, "blank-session", "name: '   '\nuser_named: true\n")
	writeWorkspaceMetadata(t, root, "malformed-session", "name: [\nuser_named: true\n")
	writeWorkspaceMetadata(t, root, "oversized-session", "name: "+string(bytes.Repeat([]byte("x"), maxWorkspaceMetadataSize))+"\nuser_named: true\n")
	writeWorkspaceMetadata(t, root, "bad session id", "name: ignored\nuser_named: true\n")

	titles := ScanUserTitles()
	if len(titles) != 1 {
		t.Fatalf("ScanUserTitles() = %v, want one authoritative title", titles)
	}
	if got := titles["renamed-session"]; got != "Fix: quoted title" {
		t.Fatalf("renamed title = %q, want %q", got, "Fix: quoted title")
	}
}

func TestScanUserTitlesRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DISPATCH_SESSION_STATE", root)

	target := filepath.Join(root, "target.yaml")
	if err := os.WriteFile(target, []byte("name: linked title\nuser_named: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(root, "linked-session")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(sessionDir, "workspace.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if titles := ScanUserTitles(); len(titles) != 0 {
		t.Fatalf("ScanUserTitles() followed symlink: %v", titles)
	}
}

func TestStoreUsesUserTitlesAcrossReadPaths(t *testing.T) {
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	store.userTitleSource = func() map[string]string {
		return map[string]string{"session-1": "Authoritative renamed title"}
	}

	seedSession(t, store.db, "session-1", "/repo", "owner/repo", "main", "Stale indexed summary", "", "")
	seedTurn(t, store.db, "session-1", 0, "hello", "world", "")

	sessions, err := store.ListSessions(
		context.Background(),
		FilterOptions{Query: "renamed title"},
		SortOptions{Field: SortByUpdated, Order: Descending},
		10,
	)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].Summary != "Authoritative renamed title" {
		t.Fatalf("ListSessions() = %#v, want renamed session", sessions)
	}

	byID, err := store.ListSessionsByIDs(context.Background(), []string{"session-1"})
	if err != nil {
		t.Fatalf("ListSessionsByIDs() error = %v", err)
	}
	if len(byID) != 1 || byID[0].Summary != "Authoritative renamed title" {
		t.Fatalf("ListSessionsByIDs() = %#v, want authoritative title", byID)
	}

	detail, err := store.GetSession(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if detail.Session.Summary != "Authoritative renamed title" {
		t.Fatalf("GetSession().Summary = %q, want authoritative title", detail.Session.Summary)
	}

	excluded, err := store.ListSessions(
		context.Background(),
		FilterOptions{ExcludedWords: []string{"renamed"}},
		SortOptions{Field: SortByUpdated, Order: Descending},
		10,
	)
	if err != nil {
		t.Fatalf("ListSessions(excluded) error = %v", err)
	}
	if len(excluded) != 0 {
		t.Fatalf("ListSessions(excluded) = %#v, want no sessions", excluded)
	}
}

func TestListSessionsMatchesUserTitleInQuickAndDeepSearch(t *testing.T) {
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	store.userTitleSource = func() map[string]string {
		return map[string]string{"session-1": "Renamed Café Session"}
	}

	seedSession(t, store.db, "session-1", "/repo", "owner/repo", "main", "Stale indexed summary", "", "")
	seedTurn(t, store.db, "session-1", 0, "unrelated question", "unrelated answer", "")

	for _, deep := range []bool{false, true} {
		name := "quick"
		if deep {
			name = "deep"
		}
		t.Run(name, func(t *testing.T) {
			sessions, err := store.ListSessions(
				context.Background(),
				FilterOptions{Query: "CAFÉ SESSION", DeepSearch: deep},
				SortOptions{Field: SortByUpdated, Order: Descending},
				10,
			)
			if err != nil {
				t.Fatalf("ListSessions() error = %v", err)
			}
			if len(sessions) != 1 || sessions[0].Summary != "Renamed Café Session" {
				t.Fatalf("ListSessions() = %#v, want user-title match", sessions)
			}
		})
	}
}

func TestListSessionsSortsAndLimitsByUserTitle(t *testing.T) {
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	store.userTitleSource = func() map[string]string {
		return map[string]string{"session-a": "Zulu"}
	}

	seedSession(t, store.db, "session-a", "/repo", "owner/repo", "main", "Alpha", "", "")
	seedSession(t, store.db, "session-b", "/repo", "owner/repo", "main", "Beta", "", "")
	seedSession(t, store.db, "session-c", "/repo", "owner/repo", "main", "Charlie", "", "")
	for _, id := range []string{"session-a", "session-b", "session-c"} {
		seedTurn(t, store.db, id, 0, "hello", "world", "")
	}

	tests := []struct {
		name  string
		order SortOrder
		want  []string
	}{
		{name: "ascending", order: Ascending, want: []string{"session-b", "session-c"}},
		{name: "descending", order: Descending, want: []string{"session-a", "session-c"}},
		{name: "default is descending", want: []string{"session-a", "session-c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions, err := store.ListSessions(
				context.Background(),
				FilterOptions{},
				SortOptions{Field: SortByName, Order: tt.order},
				2,
			)
			if err != nil {
				t.Fatalf("ListSessions() error = %v", err)
			}
			if len(sessions) != len(tt.want) {
				t.Fatalf("ListSessions() returned %d sessions, want %d", len(sessions), len(tt.want))
			}
			got := []string{sessions[0].ID, sessions[1].ID}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("sorted IDs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupSessionsSortsAndLimitsByUserTitle(t *testing.T) {
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	store.userTitleSource = func() map[string]string {
		return map[string]string{"session-a": "Zulu"}
	}

	seedSession(t, store.db, "session-a", "/a", "repo-a", "main", "Alpha", "", "")
	seedSession(t, store.db, "session-b", "/b", "repo-b", "main", "Beta", "", "")
	seedSession(t, store.db, "session-c", "/c", "repo-c", "main", "Charlie", "", "")
	for _, id := range []string{"session-a", "session-b", "session-c"} {
		seedTurn(t, store.db, id, 0, "hello", "world", "")
	}

	groups, err := store.GroupSessions(
		context.Background(),
		PivotByRepo,
		FilterOptions{},
		SortOptions{Field: SortByName, Order: Ascending},
		2,
	)
	if err != nil {
		t.Fatalf("GroupSessions() error = %v", err)
	}
	var got []string
	for _, group := range groups {
		for _, session := range group.Sessions {
			got = append(got, session.ID)
		}
	}

	slices.Sort(got)
	if want := []string{"session-b", "session-c"}; !slices.Equal(got, want) {
		t.Fatalf("grouped IDs = %v, want %v", got, want)
	}
}

func TestStoreCachesAndInvalidatesUserTitles(t *testing.T) {
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })

	title := "First title"
	calls := 0
	store.userTitleSource = func() map[string]string {
		calls++
		return map[string]string{"session-1": title}
	}
	seedSession(t, store.db, "session-1", "/repo", "owner/repo", "main", "Stale", "", "")
	seedTurn(t, store.db, "session-1", 0, "hello", "world", "")

	for range 2 {
		sessions, err := store.ListSessions(
			context.Background(),
			FilterOptions{},
			SortOptions{Field: SortByUpdated, Order: Descending},
			10,
		)
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}
		if sessions[0].Summary != "First title" {
			t.Fatalf("cached title = %q, want %q", sessions[0].Summary, "First title")
		}
	}
	if calls != 1 {
		t.Fatalf("title source calls = %d, want 1 before invalidation", calls)
	}

	title = "Second title"
	store.InvalidateUserTitles()
	sessions, err := store.ListSessions(
		context.Background(),
		FilterOptions{},
		SortOptions{Field: SortByUpdated, Order: Descending},
		10,
	)
	if err != nil {
		t.Fatalf("ListSessions() after invalidation error = %v", err)
	}
	if sessions[0].Summary != "Second title" {
		t.Fatalf("refreshed title = %q, want %q", sessions[0].Summary, "Second title")
	}
	if calls != 2 {
		t.Fatalf("title source calls = %d, want 2 after invalidation", calls)
	}
}
