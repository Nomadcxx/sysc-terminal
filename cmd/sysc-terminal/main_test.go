package main

import "testing"

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
