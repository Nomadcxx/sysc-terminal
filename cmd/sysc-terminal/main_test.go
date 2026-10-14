package main

import (
	"testing"

	"github.com/Nomadcxx/sysc-terminal/internal/wayland"
)

func TestParseArgsLongIPCFlags(t *testing.T) {
	got, err := parseArgs([]string{
		"--ipc-socket", "/run/user/1000/terminal.sock",
		"--output", "DP-1", "--effect", "rain", "--theme", "dracula",
		"--file", "/home/user/.config/art.txt", "--font", "/tmp/font.ttf",
	})
	if err != nil {
		t.Fatalf("parse args: %v", err)
	}
	if got.owner.Output != "DP-1" || got.owner.Effect != "rain" || got.owner.Theme != "dracula" ||
		got.owner.Artwork != "/home/user/.config/art.txt" || got.owner.Font != "/tmp/font.ttf" ||
		got.socket != "/run/user/1000/terminal.sock" {
		t.Fatalf("parsed flags = %+v", got)
	}
}

func TestParseArgsHelpAndListDoNotRequireRuntimeFlags(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--list"}} {
		if _, err := parseArgs(args); err != nil {
			t.Errorf("parse %v: %v", args, err)
		}
	}
}

func TestParseArgsRequiresSocketAndOutput(t *testing.T) {
	if _, err := parseArgs(nil); err == nil {
		t.Fatal("runtime started without IPC socket and output")
	}
	if _, err := parseArgs([]string{"-I", "/tmp/a.sock"}); err == nil {
		t.Fatal("runtime started without output")
	}
}

func TestWallpaperBlurFlag(t *testing.T) {
	for _, value := range []string{"0", "16", "64"} {
		got, err := parseArgs([]string{"--output", "DP-1", "-I", "/tmp/control.sock", "--blur", value})
		if err != nil {
			t.Fatalf("valid blur %s: %v", value, err)
		}
		if got.owner.BlurRadius < 0 || got.owner.BlurRadius > 64 {
			t.Fatal("blur outside range")
		}
	}
	for _, value := range []string{"-1", "65", "oops"} {
		if _, err := parseArgs([]string{"--list", "--blur", value}); err == nil {
			t.Fatalf("invalid blur %s accepted", value)
		}
	}
}

func TestFPSFlagSetsTheStartUpRate(t *testing.T) {
	got, err := parseArgs([]string{"--output", "DP-1", "-I", "/tmp/control.sock", "--fps", "12"})
	if err != nil {
		t.Fatalf("parse args: %v", err)
	}
	if got.owner.FPS != 12 {
		t.Fatalf("--fps 12 set the rate to %d", got.owner.FPS)
	}
	got, err = parseArgs([]string{"--output", "DP-1", "-I", "/tmp/control.sock"})
	if err != nil {
		t.Fatalf("parse args: %v", err)
	}
	if got.owner.FPS != wayland.MaxFPS {
		t.Fatalf("default rate = %d, want %d", got.owner.FPS, wayland.MaxFPS)
	}
	for _, value := range []string{"0", "21", "-3", "oops"} {
		if _, err := parseArgs([]string{"--output", "DP-1", "-I", "/tmp/control.sock", "--fps", value}); err == nil {
			t.Fatalf("--fps %s accepted", value)
		}
	}
}
