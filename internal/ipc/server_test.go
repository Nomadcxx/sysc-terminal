package ipc

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var errNop = errors.New("nope")

type fakeState struct {
	ready               bool
	playing             bool
	id, theme, file     string
	paused, reset, stop bool
	changeErr           error
}

func (f *fakeState) Status() (playing bool, id, theme string) {
	return f.playing && !f.paused, f.id, f.theme
}
func (f *fakeState) SetPaused(p bool) { f.paused = p }
func (f *fakeState) Reset()           { f.reset = true }
func (f *fakeState) Stop()            { f.stop = true }
func (f *fakeState) Change(id, theme, file string) error {
	if f.changeErr != nil {
		return f.changeErr
	}
	f.id, f.theme, f.file = id, theme, file
	return nil
}

func start(t *testing.T, st State) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.sock")
	s, err := Listen(path, st)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func roundtrip(t *testing.T, path, line string) (string, error) {
	t.Helper()
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(c, line+"\n"); err != nil {
		return "", err
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

func TestQueryBeforeReadyFails(t *testing.T) {
	st := &fakeState{playing: true, id: "fire", theme: "nord"}
	s, path := start(t, st)
	got, err := roundtrip(t, path, "query")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if !strings.HasPrefix(got, "ERR") {
		t.Fatalf("query before ready: %q", got)
	}
	s.SetReady(true)
	got, err = roundtrip(t, path, "query")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if !strings.HasPrefix(got, "STATUS:") {
		t.Fatalf("query after ready: %q", got)
	}
}

func TestPauseResumeIdempotent(t *testing.T) {
	st := &fakeState{playing: true, id: "fire", theme: "nord"}
	s, path := start(t, st)
	s.SetReady(true)
	for i := 0; i < 2; i++ {
		got, err := roundtrip(t, path, "pause")
		if err != nil || got != "OK" {
			t.Fatalf("pause %d: %q %v", i, got, err)
		}
	}
	if !st.paused {
		t.Fatal("pause did not stick")
	}
	for i := 0; i < 2; i++ {
		got, err := roundtrip(t, path, "resume")
		if err != nil || got != "OK" {
			t.Fatalf("resume %d: %q %v", i, got, err)
		}
	}
	if st.paused {
		t.Fatal("resume left paused")
	}
}

func TestChangeEffect(t *testing.T) {
	st := &fakeState{id: "fire", theme: "nord"}
	s, path := start(t, st)
	s.SetReady(true)
	got, err := roundtrip(t, path, "change effect rain theme dracula")
	if err != nil || got != "OK" {
		t.Fatalf("change: %q %v", got, err)
	}
	if st.id != "rain" || st.theme != "dracula" {
		t.Fatalf("state %s %s", st.id, st.theme)
	}
	st.changeErr = errNop
	got, err = roundtrip(t, path, "change effect nope theme nord")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if !strings.HasPrefix(got, "ERR") {
		t.Fatalf("unknown effect: %q", got)
	}
}

func TestOversizeLineCloses(t *testing.T) {
	st := &fakeState{}
	s, path := start(t, st)
	s.SetReady(true)
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write(append(make([]byte, 4097), '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 8)
	n, err := c.Read(buf)
	if err == nil && n > 0 {
		t.Fatalf("oversize line still answered: %q", buf[:n])
	}
}

func TestFileNewlineRejected(t *testing.T) {
	if err := CheckFile("/tmp/foo\nbar"); err == nil {
		t.Fatal("file path with newline accepted")
	}
}

func TestFileRelativeRejected(t *testing.T) {
	if err := CheckFile("relative/art.txt"); err == nil {
		t.Fatal("relative file accepted")
	}
}

func TestUnknownVerb(t *testing.T) {
	st := &fakeState{}
	s, path := start(t, st)
	s.SetReady(true)
	got, err := roundtrip(t, path, "explode")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if !strings.HasPrefix(got, "ERR") {
		t.Fatalf("unknown verb: %q", got)
	}
}

func TestListenMode(t *testing.T) {
	st := &fakeState{}
	_, path := start(t, st)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o, want 0600", fi.Mode().Perm())
	}
}
