package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/effect"
	"github.com/Nomadcxx/sysc-terminal/internal/ipc"
	"github.com/Nomadcxx/sysc-terminal/internal/wayland"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type commandLine struct {
	socket string
	owner  wayland.Config
	list   bool
	help   bool
}

func parseArgs(args []string) (commandLine, error) {
	var parsed commandLine
	parsed.owner.Effect = "fire"
	parsed.owner.Theme = "nord"
	fs := flag.NewFlagSet("sysc-terminal", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { fmt.Fprint(os.Stdout, help(usage)) }
	fs.StringVar(&parsed.socket, "I", "", "control socket path")
	fs.StringVar(&parsed.socket, "ipc-socket", "", "control socket path")
	fs.StringVar(&parsed.owner.Output, "output", "", "output connector name")
	fs.StringVar(&parsed.owner.Effect, "effect", "fire", "sysc-Go effect id")
	fs.StringVar(&parsed.owner.Theme, "theme", "nord", "sysc-Go theme name")
	fs.StringVar(&parsed.owner.Artwork, "file", "", "artwork file for text effects")
	fs.StringVar(&parsed.owner.Font, "font", "", "monospace font path")
	fs.BoolVar(&parsed.list, "list", false, "list effects and themes")
	fs.BoolVar(&parsed.help, "help", false, "show this help")
	fs.BoolVar(&parsed.help, "h", false, "show this help")
	if err := fs.Parse(args); err != nil {
		return parsed, err
	}
	if fs.NArg() != 0 {
		return parsed, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if parsed.help || parsed.list {
		return parsed, nil
	}
	if parsed.socket == "" {
		return parsed, errors.New("--ipc-socket is required")
	}
	if parsed.owner.Output == "" {
		return parsed, errors.New("--output is required")
	}
	return parsed, nil
}

func run(args []string) error {
	parsed, err := parseArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if parsed.help {
		fmt.Fprint(os.Stdout, help(usage))
		return nil
	}
	if parsed.list {
		fmt.Fprint(os.Stdout, effect.List())
		return nil
	}

	owner, err := wayland.NewOwner(parsed.owner)
	if err != nil {
		return err
	}
	defer owner.Close()
	state := &runtimeState{owner: owner}
	server, err := ipc.Listen(parsed.socket, state)
	if err != nil {
		return err
	}
	defer server.Close()

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	wakeDone := make(chan struct{})
	defer close(wakeDone)
	go func() {
		select {
		case <-signalCtx.Done():
			owner.Wake()
		case <-wakeDone:
		}
	}()
	return owner.Run(signalCtx, func() { server.SetReady(true) })
}

type runtimeState struct{ owner *wayland.Owner }

func (s *runtimeState) request(req wayland.ControlRequest) (wayland.ControlReply, error) {
	req.Reply = make(chan wayland.ControlReply, 1)
	if err := s.owner.Submit(req); err != nil {
		return wayland.ControlReply{}, err
	}
	timer := time.NewTimer(1800 * time.Millisecond)
	defer timer.Stop()
	select {
	case reply := <-req.Reply:
		return reply, reply.Err
	case <-s.owner.Done():
		return wayland.ControlReply{}, wayland.ErrOwnerClosed
	case <-timer.C:
		return wayland.ControlReply{}, errors.New("wayland: control request timed out")
	}
}

func (s *runtimeState) Status() (bool, string, string) {
	reply, err := s.request(wayland.ControlRequest{Op: wayland.ControlStatus})
	if err != nil {
		return false, "", ""
	}
	return reply.Playing, reply.ID, reply.Theme
}

func (s *runtimeState) SetPaused(paused bool) error {
	_, err := s.request(wayland.ControlRequest{Op: wayland.ControlPause, Paused: paused})
	return err
}

func (s *runtimeState) Change(id, theme, file string) error {
	_, err := s.request(wayland.ControlRequest{Op: wayland.ControlChange, ID: id, Theme: theme, File: file})
	return err
}

func (s *runtimeState) Reset() error {
	_, err := s.request(wayland.ControlRequest{Op: wayland.ControlReset})
	return err
}

func (s *runtimeState) Stop() error {
	_, err := s.request(wayland.ControlRequest{Op: wayland.ControlStop})
	return err
}

const usage = `sysc-terminal — Niri Background-layer terminal-effect wallpaper

Usage: sysc-terminal [flags]

  -I, --ipc-socket path   control socket
      --output name       output connector (one process per output)
      --effect id         sysc-Go effect id (default fire)
      --theme name        sysc-Go theme (default nord)
      --file path         artwork for text effects
      --font path         monospace font path
      --list              list effects and themes
      --help              this help
`
