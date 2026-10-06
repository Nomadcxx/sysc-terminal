package raster

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// JetBrains Mono 2.304 Regular, SIL Open Font License 1.1, see fonts/OFL.txt.
// Bundled so a machine with no probed font still renders; it is the LAST
// resort, below every system path, because a font the user installed wins.
//
//go:embed fonts/JetBrainsMono-Regular.ttf
var bundledFont []byte

// bundledFontFile is the cache name for the materialised embedded font.
const bundledFontFile = "JetBrainsMono-Regular.ttf"

// defaultFontRoots are the directory trees probed for a system font, in the
// order they are walked. The explicit paths in defaultFonts come first; these
// cover the distros that keep the same fonts under a different directory.
var defaultFontRoots = []string{
	"/usr/share/fonts/TTF",
	"/usr/share/fonts/truetype/dejavu",
	"/usr/share/fonts/truetype/noto",
	"/usr/share/fonts/truetype/liberation",
	"/usr/share/fonts/noto",
}

// defaultFonts are probed by full path first: the exact files a mono shell
// wants, across the Arch/Nix, Fedora and Debian/Ubuntu layouts. The old list
// only had the first two directories, so a Debian install (fonts-dejavu-core)
// found nothing and the renderer refused to start (sysc-terminal#3).
var defaultFonts = []string{
	"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf",
	"/usr/share/fonts/TTF/JetBrainsMono-Regular.ttf",
	"/usr/share/fonts/noto/NotoSansMono-Regular.ttf",
	"/usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
	"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
}

// monoFontNames are the file names a directory walk accepts, best first. Only
// .ttf is walked: .otf/.ttc need an index the loader does not expose for a
// whole directory, and the explicit paths above cover the common faces.
var monoFontNames = []string{
	"JetBrainsMonoNerdFont-Regular.ttf",
	"JetBrainsMono-Regular.ttf",
	"NotoSansMono-Regular.ttf",
	"DejaVuSansMono.ttf",
	"LiberationMono-Regular.ttf",
}

func FindFont(explicit string) (string, error) {
	if explicit != "" {
		fi, err := os.Stat(explicit)
		if err != nil {
			return "", err
		}
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("raster: font %q is not a regular file", explicit)
		}
		return explicit, nil
	}
	return findFontIn(defaultFontRoots)
}

// findFontIn resolves a usable font path: the explicit defaults, then a
// bounded walk of the roots, then the bundled copy materialised into the user
// cache. Exported callers reach it through FindFont.
func findFontIn(roots []string) (string, error) {
	for _, path := range defaultFonts {
		if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
			return path, nil
		}
	}
	for _, root := range roots {
		for _, name := range monoFontNames {
			path := filepath.Join(root, name)
			if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
				return path, nil
			}
		}
	}
	// ponytail: one bounded level of walking, not a fontconfig database. It
	// covers a font installed outside the standard directories without adding a
	// dependency or a subprocess; replace with an fc-match query if a user ever
	// needs a face none of these names cover.
	for _, root := range roots {
		if font, ok := firstMonoFont(root, 2); ok {
			return font, nil
		}
	}
	path, err := materialiseBundledFont()
	if err != nil {
		return "", fmt.Errorf("raster: no supported font found; tried %v and the bundled fallback: %w", defaultFonts, err)
	}
	return path, nil
}

// firstMonoFont returns the first .ttf under dir, at most depth levels deep.
func firstMonoFont(dir string, depth int) (string, bool) {
	if depth < 0 {
		return "", false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var dirs []string
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		switch {
		case e.IsDir():
			dirs = append(dirs, path)
		case filepath.Ext(e.Name()) == ".ttf":
			return path, true
		}
	}
	for _, sub := range dirs {
		if path, ok := firstMonoFont(sub, depth-1); ok {
			return path, ok
		}
	}
	return "", false
}

// materialiseBundledFont writes the embedded font into the user cache,
// atomically, and returns its path. Written on first use and reused after.
func materialiseBundledFont() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate the user cache: %w", err)
	}
	dir := filepath.Join(cache, "sysc-terminal", "fonts")
	path := filepath.Join(dir, bundledFontFile)
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() && fi.Size() == int64(len(bundledFont)) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, bundledFontFile+".*")
	if err != nil {
		return "", fmt.Errorf("stage %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bundledFont); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write the bundled font: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("install %s: %w", path, err)
	}
	return path, nil
}
