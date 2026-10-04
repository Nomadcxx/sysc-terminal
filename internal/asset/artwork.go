package asset

import (
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ponytail: cap artwork at 64 KiB; larger assets require a measured budget.
const maxArtworkBytes = 64 << 10

func CheckFile(path string) error {
	if path == "" || !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\n\r") {
		return fmt.Errorf("file")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("file not absolute")
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return fmt.Errorf("file not allowed")
	}
	if !withinAllowedRoots(filepath.Clean(path), allowedRoots(home)) {
		return fmt.Errorf("file not allowed")
	}
	return nil
}

func ReadArtwork(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("artwork: home directory unavailable")
	}
	return ReadArtworkAt(path, home)
}

func ReadArtworkAt(path, home string) (string, error) {
	if err := checkFile(path, home); err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	realPath, err = filepath.Abs(realPath)
	if err != nil || !withinAllowedRoots(filepath.Clean(realPath), canonicalRoots(home)) {
		return "", fmt.Errorf("artwork: resolved path not allowed")
	}
	fd, err := unix.Open(realPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(fd), realPath)
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("artwork: not a regular file")
	}
	if fi.Size() > maxArtworkBytes {
		return "", fmt.Errorf("artwork: exceeds %d bytes", maxArtworkBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxArtworkBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxArtworkBytes {
		return "", fmt.Errorf("artwork: exceeds %d bytes", maxArtworkBytes)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("artwork: must be UTF-8")
	}
	return string(data), nil
}

func checkFile(path, home string) error {
	if path == "" || home == "" || !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\n\r") {
		return fmt.Errorf("file")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("file not absolute")
	}
	if !withinAllowedRoots(filepath.Clean(path), allowedRoots(home)) {
		return fmt.Errorf("file not allowed")
	}
	return nil
}

func allowedRoots(home string) []string {
	return []string{
		filepath.Join(home, ".config"),
		filepath.Join(home, ".local", "share"),
		"/usr/share",
		"/usr/local/share",
	}
}

func canonicalRoots(home string) []string {
	roots := allowedRoots(home)
	for i, root := range roots {
		if real, err := filepath.EvalSymlinks(root); err == nil {
			roots[i] = real
		}
	}
	return roots
}

func withinAllowedRoots(path string, roots []string) bool {
	for _, root := range roots {
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
