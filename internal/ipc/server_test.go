package ipc

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var errNop = errors.New("nope")

type fakeState struct {
	ready                                  bool
	playing                                bool
	id, theme, file                        string
	paused, reset, stop                    bool
	pauseErr, resetErr, stopErr, changeErr error
	statusEntered                          chan struct{}
	statusRelease                          <-chan struct{}
}

func (f *fakeState) Status() (playing bool, id, theme string) {
	if f.statusEntered != nil {
		f.statusEntered <- struct{}{}
		<-f.statusRelease
	}
	return f.playing && !f.paused, f.id, f.theme
}
func (f *fakeState) SetPaused(p bool) error {
	if f.pauseErr != nil {
		return f.pauseErr
	}
	f.paused = p
	return nil
}
func (f *fakeState) Reset() error {
	if f.resetErr != nil {
		return f.resetErr
	}
	f.reset = true
	return nil
}
func (f *fakeState) Stop() error {
	if f.stopErr != nil {
		return f.stopErr
	}
	f.stop = true
	return nil
}
func (f *fakeState) Change(id, theme, file string) error {
	if f.changeErr != nil {
		return f.changeErr
	}
	f.id, f.theme, f.file = id, theme, file
	return nil
}

func start(t *testing.T, st State) (*Server, string) {
	t.Helper()
	dir := privateTempDir(t)
	path := filepath.Join(dir, "t.sock")
	s, err := Listen(path, st)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func privateTempDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ipc")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
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

func TestRejectsConnectionsWhenHandlerLimitReached(t *testing.T) {
	release := make(chan struct{})
	st := &fakeState{
		playing:       true,
		id:            "fire",
		theme:         "nord",
		statusEntered: make(chan struct{}, maxHandlers),
		statusRelease: release,
	}
	s, path := start(t, st)
	s.SetReady(true)
	t.Cleanup(func() { close(release) })

	clients := make([]net.Conn, 0, maxHandlers)
	t.Cleanup(func() {
		for _, c := range clients {
			_ = c.Close()
		}
	})
	for i := 0; i < maxHandlers; i++ {
		c, err := net.DialTimeout("unix", path, time.Second)
		if err != nil {
			t.Fatalf("dial handler %d: %v", i, err)
		}
		clients = append(clients, c)
		if _, err := io.WriteString(c, "query\n"); err != nil {
			t.Fatalf("write handler %d: %v", i, err)
		}
		select {
		case <-st.statusEntered:
		case <-time.After(time.Second):
			t.Fatalf("handler %d did not reach status", i)
		}
	}

	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("dial excess handler: %v", err)
	}
	defer c.Close()
	// The server closes the excess connection as soon as it accepts it, so the
	// write below races that close: it either lands in the socket buffer or
	// hits EPIPE. Either is the rejection; the read assertion below is what
	// proves the connection was not served.
	if _, err := io.WriteString(c, "query\n"); err != nil && !errors.Is(err, syscall.EPIPE) {
		t.Fatalf("write excess handler: %v", err)
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	var response [1]byte
	if n, err := c.Read(response[:]); n != 0 || (!errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET)) {
		t.Fatalf("excess connection remained active: read %q, %v", response[:n], err)
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

func TestChangeEffectAcceptsArtworkPathWithSpaces(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config", "terminal art", "my artwork.txt")
	st := &fakeState{id: "fire-text", theme: "nord"}
	_, socket := start(t, st)
	got, err := roundtrip(t, socket, "change effect fire-text theme nord file "+path)
	if err != nil || got != "OK" {
		t.Fatalf("change with spaced artwork path: %q %v", got, err)
	}
	if st.file != path {
		t.Fatalf("artwork path = %q, want %q", st.file, path)
	}
}

func TestWorkerControlErrorsAreReturned(t *testing.T) {
	st := &fakeState{pauseErr: errNop, resetErr: errNop, stopErr: errNop}
	s, path := start(t, st)
	s.SetReady(true)
	for _, command := range []string{"pause", "reset", "stop"} {
		got, err := roundtrip(t, path, command)
		if err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		if got != "ERR nope" {
			t.Errorf("%s reply = %q, want worker error", command, got)
		}
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

func TestFileInvalidUTF8Rejected(t *testing.T) {
	if err := CheckFile("/home/user/.config/\xff.txt"); err == nil {
		t.Fatal("invalid UTF-8 file path accepted")
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

func TestListenDoesNotRemoveRegularFile(t *testing.T) {
	path := filepath.Join(privateTempDir(t), "t.sock")
	want := []byte("keep this file")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path, &fakeState{}); err == nil {
		t.Fatal("listen replaced an existing regular file")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(want) {
		t.Fatalf("existing file changed: %q, %v", got, err)
	}
}

func TestListenRefusesInsecureDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(filepath.Join(dir, "t.sock"), &fakeState{}); err == nil {
		t.Fatal("listener accepted a socket in a non-private directory")
	}
}

func TestListenReclaimsStaleSocket(t *testing.T) {
	path := filepath.Join(privateTempDir(t), "t.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	stale := l.(*net.UnixListener)
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Listen(path, &fakeState{})
	if err != nil {
		t.Fatalf("listen did not replace a stale owned socket: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestListenDoesNotStealLiveSocket(t *testing.T) {
	st := &fakeState{playing: true, id: "fire", theme: "nord"}
	first, path := start(t, st)
	first.SetReady(true)
	if _, err := Listen(path, &fakeState{}); err == nil {
		t.Fatal("second listener replaced a live socket")
	}
	got, err := roundtrip(t, path, "query")
	if err != nil || !strings.HasPrefix(got, "STATUS:") {
		t.Fatalf("first listener stopped answering: %q, %v", got, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseDoesNotRemoveReplacedPath(t *testing.T) {
	s, path := start(t, &fakeState{})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	want := []byte("replacement")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(want) {
		t.Fatalf("close removed replacement: %q, %v", got, err)
	}
}

func TestReadArtworkRejectsSymlinkEscapeAndOversize(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	allowed := filepath.Join(home, ".config")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readArtwork(link, home); err == nil {
		t.Fatal("symlink escaping the allowlist was accepted")
	}

	large := filepath.Join(allowed, "large.txt")
	if err := os.WriteFile(large, make([]byte, maxArtworkBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readArtwork(large, home); err == nil {
		t.Fatal("oversized artwork was accepted")
	}
}
