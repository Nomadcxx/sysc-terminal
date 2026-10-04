package ipc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const maxLine = 4096

// ponytail: 16 concurrent calls cover normal control bursts; excess clients close immediately.
const maxHandlers = 16

// ponytail: 64 KiB is ample for text artwork and bounds retained input before it reaches an effect.
const maxArtworkBytes = 64 << 10

type State interface {
	Status() (playing bool, id, theme string)
	SetPaused(bool) error
	Change(id, theme, file string) error
	Reset() error
	Stop() error
}

type Server struct {
	l         net.Listener
	path      string
	pathInfo  os.FileInfo
	st        State
	handlers  chan struct{}
	mu        sync.Mutex
	closeOnce sync.Once
	closeErr  error
	ready     bool
}

func Listen(path string, st State) (*Server, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\n\r") {
		return nil, fmt.Errorf("ipc: invalid socket path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := checkSocketDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		_ = l.Close()
		return nil, err
	}
	if fi.Mode()&os.ModeSocket == 0 || !ownedByCurrentUID(fi) {
		_ = l.Close()
		return nil, fmt.Errorf("ipc: socket path was replaced during creation")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	fiAfterChmod, err := os.Lstat(path)
	if err != nil || !os.SameFile(fi, fiAfterChmod) {
		_ = l.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("ipc: socket path changed during creation")
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	s := &Server{l: l, path: path, pathInfo: fi, st: st, handlers: make(chan struct{}, maxHandlers)}
	go s.serve()
	return s, nil
}

func (s *Server) SetReady(v bool) {
	s.mu.Lock()
	s.ready = v
	s.mu.Unlock()
}

func (s *Server) Close() error {
	if s == nil || s.l == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = s.l.Close()
		if s.closeErr != nil {
			return
		}
		fi, err := os.Lstat(s.path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			s.closeErr = err
			return
		}
		if os.SameFile(s.pathInfo, fi) {
			s.closeErr = os.Remove(s.path)
		}
	})
	return s.closeErr
}

func checkSocketDir(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.IsDir() || !ownedByCurrentUID(fi) || fi.Mode().Perm()&0o077 != 0 {
		st, _ := fi.Sys().(*syscall.Stat_t)
		var uid any
		if st != nil {
			uid = st.Uid
		}
		return fmt.Errorf("ipc: socket directory must be owned by this uid and private (mode=%04o uid=%v current=%d sys=%T)", fi.Mode().Perm(), uid, os.Getuid(), fi.Sys())
	}
	return nil
}

func removeStaleSocket(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 || !ownedByCurrentUID(fi) {
		return fmt.Errorf("ipc: refusing to replace existing non-owned socket path")
	}
	c, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond)
	if dialErr == nil {
		_ = c.Close()
		return fmt.Errorf("ipc: socket is already in use")
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("ipc: cannot determine whether existing socket is stale: %w", dialErr)
	}
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(fi, current) {
		return fmt.Errorf("ipc: socket path changed while checking stale socket")
	}
	return os.Remove(path)
}

func ownedByCurrentUID(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid())
}

func (s *Server) serve() {
	for {
		c, err := s.l.Accept()
		if err != nil {
			return
		}
		select {
		case s.handlers <- struct{}{}:
			go func() {
				defer func() { <-s.handlers }()
				s.handle(c)
			}()
		default:
			_ = c.Close()
		}
	}
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	if !sameUID(c) {
		return
	}
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	r := bufio.NewReaderSize(c, maxLine)
	line, err := r.ReadSlice('\n')
	if err != nil {
		return
	}
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	ack := s.dispatch(string(line))
	if ack == "" {
		return
	}
	_, _ = c.Write([]byte(ack + "\n"))
}

func (s *Server) dispatch(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "ERR empty"
	}
	switch fields[0] {
	case "query":
		s.mu.Lock()
		ready := s.ready
		s.mu.Unlock()
		if !ready {
			return "ERR not ready"
		}
		playing, id, theme := s.st.Status()
		state := "paused"
		if playing {
			state = "playing"
		}
		return fmt.Sprintf("STATUS: %s effect %s theme %s", state, id, theme)
	case "pause":
		if err := s.st.SetPaused(true); err != nil {
			return errorReply(err)
		}
		return "OK"
	case "resume":
		if err := s.st.SetPaused(false); err != nil {
			return errorReply(err)
		}
		return "OK"
	case "reset":
		if err := s.st.Reset(); err != nil {
			return errorReply(err)
		}
		return "OK"
	case "stop", "quit":
		if err := s.st.Stop(); err != nil {
			return errorReply(err)
		}
		return "OK"
	case "change":
		id, theme, file, err := parseChange(line)
		if err != nil {
			return "ERR " + err.Error()
		}
		if err := s.st.Change(id, theme, file); err != nil {
			return errorReply(err)
		}
		return "OK"
	default:
		return "ERR unknown"
	}
}

func errorReply(err error) string {
	message := strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error())
	if len(message) > maxLine-4 {
		message = message[:maxLine-4]
	}
	return "ERR " + message
}

func parseChange(line string) (id, theme, file string, err error) {
	// change effect <id> theme <theme> [file <abs>]
	verb, i := nextToken(line, 0)
	effect, i := nextToken(line, i)
	id, i = nextToken(line, i)
	themeWord, i := nextToken(line, i)
	theme, i = nextToken(line, i)
	if verb != "change" || effect != "effect" || id == "" || themeWord != "theme" || theme == "" {
		return "", "", "", fmt.Errorf("usage")
	}
	fileWord, i := nextToken(line, i)
	if fileWord == "" { // trailing field whitespace is harmless
		return id, theme, "", nil
	}
	if fileWord != "file" {
		return "", "", "", fmt.Errorf("usage")
	}
	file = strings.TrimLeft(line[i:], " \t")
	if err := CheckFile(file); err != nil {
		return "", "", "", err
	}
	return id, theme, file, nil
}

func nextToken(line string, i int) (string, int) {
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	start := i
	for i < len(line) && line[i] != ' ' && line[i] != '\t' {
		i++
	}
	return line[start:i], i
}

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
	return readArtwork(path, home)
}

func readArtwork(path, home string) (string, error) {
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

func sameUID(c net.Conn) bool {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return false
	}
	var cred *unix.Ucred
	var cerr error
	err = raw.Control(func(fd uintptr) {
		cred, cerr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil || cerr != nil || cred == nil {
		return false
	}
	return cred.Uid == uint32(os.Getuid())
}
