package wayland

import (
	"os"
	"path/filepath"
	"testing"
)

func withFontRoots(t *testing.T, roots []string) {
	t.Helper()
	prev := fontRoots
	fontRoots = roots
	t.Cleanup(func() { fontRoots = prev })
}

func TestFindFontResolvesDebianLayout(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "truetype/dejavu/DejaVuSansMono.ttf")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("ttf"), 0o644); err != nil {
		t.Fatal(err)
	}
	withFontRoots(t, []string{root})

	got, err := findFont("")
	if err != nil {
		t.Fatalf("truetype-only layout refused a font: %v", err)
	}
	if got != want {
		t.Fatalf("font %q, want %q", got, want)
	}
}

func TestFindFontPrefersOrderedCandidate(t *testing.T) {
	root := t.TempDir()
	preferred := filepath.Join(root, "TTF/JetBrainsMono-Regular.ttf")
	walked := filepath.Join(root, "nested/OtherMono-Regular.otf")
	for _, p := range []string{preferred, walked} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("ttf"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	withFontRoots(t, []string{root})

	got, err := findFont("")
	if err != nil {
		t.Fatal(err)
	}
	if got != preferred {
		t.Fatalf("font %q, want the ordered candidate %q", got, preferred)
	}
}

func TestFindFontWalksRootsForUnknownLayout(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "share/fonts/x86_64-unknown/WeirdMono-Regular.ttf")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("ttf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Inter-Regular.ttf"), []byte("ttf"), 0o644); err != nil {
		t.Fatal(err)
	}
	withFontRoots(t, []string{root})

	got, err := findFont("")
	if err != nil {
		t.Fatalf("unlisted layout refused a font: %v", err)
	}
	if got != want {
		t.Fatalf("font %q, want %q", got, want)
	}
}

func TestFindFontReportsEmptyRoot(t *testing.T) {
	withFontRoots(t, []string{t.TempDir()})
	if _, err := findFont(""); err == nil {
		t.Fatal("empty font root resolved a font")
	}
}

func TestFindFontRejectsExplicitDirectory(t *testing.T) {
	if _, err := findFont(t.TempDir()); err == nil {
		t.Fatal("directory accepted as a font")
	}
}
