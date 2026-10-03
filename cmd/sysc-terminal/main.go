package main

import (
	"fmt"
	"os"

	"github.com/Nomadcxx/sysc-terminal/internal/effect"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	for _, a := range args {
		switch a {
		case "-h", "-help", "--help":
			fmt.Fprint(os.Stdout, usage)
			return nil
		case "--list":
			fmt.Fprint(os.Stdout, effect.List())
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
