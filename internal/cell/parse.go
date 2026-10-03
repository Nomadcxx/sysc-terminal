package cell

import (
	"image/color"
	"strconv"
	"strings"
	"unicode/utf8"
)

func Parse(s string, cols, rows int) (*Grid, error) {
	g := newGrid(cols, rows)
	fg, bg := DefaultFg, DefaultBg
	x, y := 0, 0
	b := []byte(s)
	for i := 0; i < len(b); {
		if b[i] == 0x1b {
			i = applyEscape(b, i, &fg, &bg)
			continue
		}
		if b[i] == 0x07 {
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		i += size
		if r == utf8.RuneError && size == 1 {
			continue
		}
		if r == '\n' {
			x, y = 0, y+1
			continue
		}
		if y >= rows || x >= cols {
			continue
		}
		g.cells[y*cols+x] = Cell{Ch: r, Fg: fg, Bg: bg}
		x++
	}
	return g, nil
}

func applyEscape(b []byte, i int, fg, bg *color.RGBA) int {
	if i+1 >= len(b) {
		return i + 1
	}
	switch b[i+1] {
	case '[':
		return parseCSI(b, i+2, fg, bg)
	case ']', 'P', '_', '^', 'X':
		return skipTerminated(b, i+2)
	default:
		return i + 2
	}
}

func skipTerminated(b []byte, i int) int {
	for i < len(b) {
		if b[i] == 0x07 {
			return i + 1
		}
		if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
			return i + 2
		}
		i++
	}
	return i
}

func parseCSI(b []byte, i int, fg, bg *color.RGBA) int {
	start := i
	for i < len(b) {
		c := b[i]
		i++
		if c >= 0x40 && c <= 0x7e {
			if c == 'm' {
				applySGR(string(b[start:i-1]), fg, bg)
			}
			return i
		}
	}
	return i
}

func applySGR(params string, fg, bg *color.RGBA) {
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); {
		p := parts[i]
		switch {
		case p == "" || p == "0":
			*fg, *bg = DefaultFg, DefaultBg
			i++
		case p == "38" && i+4 < len(parts) && parts[i+1] == "2":
			*fg = rgb(parts[i+2], parts[i+3], parts[i+4])
			i += 5
		case p == "48" && i+4 < len(parts) && parts[i+1] == "2":
			*bg = rgb(parts[i+2], parts[i+3], parts[i+4])
			i += 5
		default:
			i++
		}
	}
}

func rgb(rs, gs, bs string) color.RGBA {
	r, _ := strconv.Atoi(rs)
	g, _ := strconv.Atoi(gs)
	b, _ := strconv.Atoi(bs)
	return color.RGBA{R: clamp8(r), G: clamp8(g), B: clamp8(b), A: 255}
}

func clamp8(n int) uint8 {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return uint8(n)
}
