package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	for _, a := range args {
		if a == "-h" || a == "-help" || a == "--help" {
			fmt.Fprint(os.Stdout, usage)
			return nil
		}
	}
	return nil
}

const usage = `sysc-terminal — Niri Background-layer terminal-effect wallpaper

Usage: sysc-terminal [flags]

  -I, --ipc-socket path   control socket
      --list              list effects
      --help              this help
`
