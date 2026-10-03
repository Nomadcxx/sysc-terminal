package cell

import "image/color"

var (
	DefaultFg = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	DefaultBg = color.RGBA{A: 0}
)

type Cell struct {
	Ch     rune
	Fg, Bg color.RGBA
}

type Grid struct {
	Cols, Rows int
	cells      []Cell
}

func (g *Grid) At(x, y int) Cell {
	if g == nil || x < 0 || y < 0 || x >= g.Cols || y >= g.Rows {
		return Cell{Ch: ' ', Fg: DefaultFg, Bg: DefaultBg}
	}
	return g.cells[y*g.Cols+x]
}

func newGrid(cols, rows int) *Grid {
	g := &Grid{Cols: cols, Rows: rows, cells: make([]Cell, cols*rows)}
	for i := range g.cells {
		g.cells[i] = Cell{Ch: ' ', Fg: DefaultFg, Bg: DefaultBg}
	}
	return g
}
