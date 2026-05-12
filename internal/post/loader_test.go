package post

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoad_Fixture exercises Load against the committed testdata/vault.
// That fixture covers: a normal post with an asset, a custom-template post,
// a draft (must be skipped), and a post without an explicit slug (must
// fall back to its folder name).
func TestLoad_Fixture(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/post/ → ../../testdata/vault
	vault := filepath.Join(wd, "..", "..", "testdata", "vault")

	idx, err := Load(vault)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	expected := []string{"first-post", "custom-template", "no-slug-folder"}
	if len(idx.BySlug) != len(expected) {
		t.Errorf("expected %d posts, got %d (%v)", len(expected), len(idx.BySlug), keys(idx.BySlug))
	}
	for _, slug := range expected {
		if _, ok := idx.BySlug[slug]; !ok {
			t.Errorf("missing slug %q in index", slug)
		}
	}
	if _, ok := idx.BySlug["draft-post"]; ok {
		t.Errorf("draft-post should be excluded from index")
	}

	// Sorted is newest-first. The fixture dates are 2026-05-11
	// (custom-template), 2026-05-10 (first-post), 2026-05-09 (no-slug-folder).
	wantOrder := []string{"custom-template", "first-post", "no-slug-folder"}
	if len(idx.Sorted) != len(wantOrder) {
		t.Fatalf("sorted len=%d, want %d", len(idx.Sorted), len(wantOrder))
	}
	for i, slug := range wantOrder {
		if idx.Sorted[i].Slug != slug {
			t.Errorf("sorted[%d] = %q, want %q", i, idx.Sorted[i].Slug, slug)
		}
	}
}

func TestLoad_MissingVault(t *testing.T) {
	dir := t.TempDir() // no blog/ subdir
	idx, err := Load(dir)
	if err != nil {
		t.Fatalf("Load on empty vault: %v", err)
	}
	if len(idx.BySlug) != 0 {
		t.Errorf("expected empty index, got %v", keys(idx.BySlug))
	}
}

func TestLoad_DuplicateSlug(t *testing.T) {
	dir := t.TempDir()
	mustWritePost(t, filepath.Join(dir, "blog", "a"), "shared-slug", "2026-05-01")
	mustWritePost(t, filepath.Join(dir, "blog", "b"), "shared-slug", "2026-05-02")

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected duplicate-slug error")
	}
	if !strings.Contains(err.Error(), "duplicate slug") {
		t.Errorf("expected duplicate-slug message, got: %v", err)
	}
}

func TestLoad_SkipsDraftsSubfolder(t *testing.T) {
	dir := t.TempDir()
	mustWritePost(t, filepath.Join(dir, "blog", "real"), "real", "2026-05-01")
	mustWritePost(t, filepath.Join(dir, "blog", "_drafts", "hidden"), "hidden", "2026-05-02")

	idx, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx.BySlug["hidden"]; ok {
		t.Errorf("_drafts content leaked into index")
	}
}

func TestLoad_PropagatesParseError(t *testing.T) {
	dir := t.TempDir()
	postDir := filepath.Join(dir, "blog", "broken")
	if err := os.MkdirAll(postDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(postDir, "index.md"),
		[]byte("---\ntitle: T\nslug: s\ndate: not-a-date\npublished: true\n---\n\nbody\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error from malformed post")
	}
}

func keys(m map[string]Post) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mustWritePost(t *testing.T, dir, slug, date string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ntitle: T\nslug: " + slug + "\ndate: " + date + "\npublished: true\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
