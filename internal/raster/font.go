package raster

import (
	"fmt"
	"os"
)

func FindFont(explicit string) (string, error) {
	if explicit != "" {
		fi, err := os.Stat(explicit)
		if err != nil {
			return "", err
		}
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("raster: font %q is not a regular file", explicit)
		}
		return explicit, nil
	}
	paths := []string{
		"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf",
		"/usr/share/fonts/TTF/JetBrainsMono-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansMono-Regular.ttf",
	}
	for _, path := range paths {
		fi, err := os.Stat(path)
		if err == nil && fi.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("raster: no supported font found; tried %v", paths)
}
