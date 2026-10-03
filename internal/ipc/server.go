package ipc

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const maxLine = 4096

type State interface {
	Status() (playing bool, id, theme string)
	SetPaused(bool)
	Change(id, theme, file string) error
	Reset()
	Stop()
}

type Server struct {
	l     net.Listener
	st    State
	mu    sync.Mutex
	ready bool
}

func Listen(path string, st State) (*Server, error) {
	_ = os.Remove(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	s := &Server{l: l, st: st}
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
	return s.l.Close()
}

func (s *Server) serve() {
	for {
		c, err := s.l.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
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
		s.st.SetPaused(true)
		return "OK"
	case "resume":
		s.st.SetPaused(false)
		return "OK"
	case "reset":
		s.st.Reset()
		return "OK"
	case "stop", "quit":
		s.st.Stop()
		return "OK"
	case "change":
		id, theme, file, err := parseChange(fields[1:])
		if err != nil {
			return "ERR " + err.Error()
		}
		if err := s.st.Change(id, theme, file); err != nil {
			return "ERR " + err.Error()
		}
		return "OK"
	default:
		return "ERR unknown"
	}
}

func parseChange(args []string) (id, theme, file string, err error) {
	// change effect <id> theme <theme> [file <abs>]
	if len(args) < 4 || args[0] != "effect" || args[2] != "theme" {
		return "", "", "", fmt.Errorf("usage")
	}
	id, theme = args[1], args[3]
	if len(args) == 4 {
		return id, theme, "", nil
	}
	if len(args) != 6 || args[4] != "file" {
		return "", "", "", fmt.Errorf("usage")
	}
	file = args[5]
	if err := CheckFile(file); err != nil {
		return "", "", "", err
	}
	return id, theme, file, nil
}

func CheckFile(path string) error {
	if path == "" || strings.ContainsAny(path, "\n\r") {
		return fmt.Errorf("file")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("file not absolute")
	}
	home, _ := os.UserHomeDir()
	allow := []string{
		filepath.Join(home, ".config"),
		filepath.Join(home, ".local", "share"),
		"/usr/share",
		"/usr/local/share",
	}
	clean := filepath.Clean(path)
	for _, root := range allow {
		rel, err := filepath.Rel(root, clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil
		}
	}
	return fmt.Errorf("file not allowed")
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
