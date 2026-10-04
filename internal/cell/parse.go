package cell

import (
	"fmt"
	"image/color"
	"math"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

func Parse(s string, cols, rows int) (*Grid, error) {
	if len(s) > MaxFrameBytes {
		return nil, fmt.Errorf("cell: frame is %d bytes, limit is %d", len(s), MaxFrameBytes)
	}
	if cols <= 0 || rows <= 0 || cols > math.MaxInt/rows || cols*rows > MaxGridCells {
		return nil, fmt.Errorf("cell: invalid grid size %dx%d", cols, rows)
	}
	g := newGrid(cols, rows)
	fg, bg := DefaultFg, DefaultBg
	x, y := 0, 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = applyEscape(s, i, &fg, &bg)
			continue
		}
		if s[i] == 0x07 {
			i++
			continue
		}
		var r rune
		size := 1
		if s[i] < utf8.RuneSelf {
			r = rune(s[i])
		} else {
			r, size = utf8.DecodeRuneInString(s[i:])
		}
		i += size
		if r == utf8.RuneError && size == 1 {
			continue
		}
		if r == '\n' {
			x, y = 0, y+1
			continue
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			continue
		}
		width := 1
		if r >= utf8.RuneSelf {
			width = runewidth.RuneWidth(r)
		}
		if width == 0 {
			continue
		}
		if y < rows && x < cols && x+width <= cols {
			g.cells[y*cols+x] = Cell{Ch: r, Fg: fg, Bg: bg}
			if width == 2 {
				g.cells[y*cols+x+1] = Cell{Ch: 0, Fg: fg, Bg: bg}
			}
		}
		x += width
	}
	return g, nil
}

func applyEscape(s string, i int, fg, bg *color.RGBA) int {
	if i+1 >= len(s) {
		return i + 1
	}
	switch s[i+1] {
	case '[':
		return parseCSI(s, i+2, fg, bg)
	case ']', 'P', '_', '^', 'X':
		return skipTerminated(s, i+2)
	default:
		return i + 2
	}
}

func skipTerminated(s string, i int) int {
	for i < len(s) {
		if s[i] == 0x07 {
			return i + 1
		}
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
			return i + 2
		}
		i++
	}
	return i
}

func parseCSI(s string, i int, fg, bg *color.RGBA) int {
	start := i
	for i < len(s) {
		c := s[i]
		i++
		if c >= 0x40 && c <= 0x7e {
			if c == 'm' {
				applySGR(s[start:i-1], fg, bg)
			}
			return i
		}
	}
	return i
}

func applySGR(params string, fg, bg *color.RGBA) {
	for start := 0; start <= len(params); {
		p, next, _ := sgrField(params, start)
		switch p {
		case "", "0":
			*fg, *bg = DefaultFg, DefaultBg
		case "38", "48":
			mode, afterMode, modeOK := sgrField(params, next)
			red, afterRed, redOK := sgrField(params, afterMode)
			green, afterGreen, greenOK := sgrField(params, afterRed)
			blue, afterBlue, blueOK := sgrField(params, afterGreen)
			if modeOK && mode == "2" && redOK && greenOK && blueOK {
				if c, ok := rgb(red, green, blue); ok {
					if p == "38" {
						*fg = c
					} else {
						*bg = c
					}
				}
				start = afterBlue
				continue
			}
		}
		if next > len(params) {
			return
		}
		start = next
	}
}

func sgrField(s string, start int) (field string, next int, ok bool) {
	if start > len(s) {
		return "", 0, false
	}
	end := start
	for end < len(s) && s[end] != ';' {
		end++
	}
	return s[start:end], end + 1, true
}

func rgb(rs, gs, bs string) (color.RGBA, bool) {
	var components [3]uint8
	for i, s := range []string{rs, gs, bs} {
		if s == "" {
			return color.RGBA{}, false
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return color.RGBA{}, false
			}
		}
		n := 0
		for _, r := range s {
			n = n*10 + int(r-'0')
			if n > 255 {
				return color.RGBA{}, false
			}
		}
		components[i] = uint8(n)
	}
	return color.RGBA{R: components[0], G: components[1], B: components[2], A: 255}, true
}
