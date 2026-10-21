package cell

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
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
	reverse := false
	x, y := 0, 0
	lastRune := rune(0)
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if i+1 < len(s) && s[i+1] == '[' {
				end := i + 2
				for end < len(s) && !(s[end] >= 0x40 && s[end] <= 0x7e) {
					end++
				}
				if end < len(s) && s[end] == 'b' {
					count, ok := parseREPCount(s[i+2:end], cols-x)
					if ok && lastRune == ' ' {
						visibleFg, visibleBg := fg, bg
						if reverse {
							visibleFg, visibleBg = bg, fg
						}
						for range count {
							if y >= 0 && y < rows && x >= 0 && x < cols {
								g.cells[y*cols+x] = Cell{Ch: ' ', Fg: visibleFg, Bg: visibleBg}
							}
							x++
						}
					}
					i = end + 1
					continue
				}
			}
			i = applyEscape(s, i, &fg, &bg, &reverse)
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
			visibleFg, visibleBg := fg, bg
			if reverse {
				visibleFg, visibleBg = bg, fg
			}
			g.cells[y*cols+x] = Cell{Ch: r, Fg: visibleFg, Bg: visibleBg}
			if width == 2 {
				g.cells[y*cols+x+1] = Cell{Ch: 0, Fg: visibleFg, Bg: visibleBg}
			}
		}
		x += width
		lastRune = r
	}
	return g, nil
}

func parseREPCount(params string, remaining int) (int, bool) {
	if remaining <= 0 {
		return 0, false
	}
	if params == "" {
		return 1, true
	}
	n, err := strconv.Atoi(params)
	return n, err == nil && n > 0 && n <= remaining
}

func applyEscape(s string, i int, fg, bg *color.RGBA, reverse *bool) int {
	if i+1 >= len(s) {
		return i + 1
	}
	switch s[i+1] {
	case '[':
		return parseCSI(s, i+2, fg, bg, reverse)
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

func parseCSI(s string, i int, fg, bg *color.RGBA, reverse *bool) int {
	start := i
	for i < len(s) {
		c := s[i]
		i++
		if c >= 0x40 && c <= 0x7e {
			if c == 'm' {
				applySGR(s[start:i-1], fg, bg, reverse)
			}
			return i
		}
	}
	return i
}

func applySGR(params string, fg, bg *color.RGBA, reverse *bool) {
	for start := 0; start <= len(params); {
		p, next, _ := sgrField(params, start)
		switch p {
		case "", "0":
			*fg, *bg = DefaultFg, DefaultBg
			*reverse = false
		case "7":
			*reverse = true
		case "27":
			*reverse = false
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
