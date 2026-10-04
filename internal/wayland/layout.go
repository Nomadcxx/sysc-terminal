package wayland

import (
	"fmt"
	"math"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/Nomadcxx/sysc-terminal/internal/effect"
	"github.com/Nomadcxx/sysc-terminal/internal/raster"
)

type GridGeometry struct {
	PixelWidth, PixelHeight int
	CellWidth, CellHeight   int
	Cols, Rows              int
	Right, Bottom           int
}

func PixelSize(logical, scale120 int) (int, error) {
	if logical <= 0 || scale120 <= 0 {
		return 0, fmt.Errorf("wayland: invalid font scale %d at %d/120", logical, scale120)
	}
	size := math.Round(float64(logical) * float64(scale120) / 120)
	if size < 1 {
		size = 1
	}
	if size > math.MaxUint16 {
		return 0, fmt.Errorf("wayland: font pixel size %.0f exceeds uint16", size)
	}
	return int(size), nil
}

func DevicePixels(logical, scale120 int) (int, error) {
	if logical <= 0 || scale120 <= 0 {
		return 0, fmt.Errorf("wayland: invalid surface size %d at %d/120", logical, scale120)
	}
	size := math.Round(float64(logical) * float64(scale120) / 120)
	if size < 1 || size > math.MaxInt32 {
		return 0, fmt.Errorf("wayland: device size %.0f exceeds int32", size)
	}
	return int(size), nil
}

func ConfiguredGrid(logicalWidth, logicalHeight, scale120, cellWidth, cellHeight int) (GridGeometry, error) {
	width, err := DevicePixels(logicalWidth, scale120)
	if err != nil {
		return GridGeometry{}, err
	}
	height, err := DevicePixels(logicalHeight, scale120)
	if err != nil {
		return GridGeometry{}, err
	}
	return DeriveGrid(width, height, cellWidth, cellHeight)
}

func DeriveGrid(pixelWidth, pixelHeight, cellWidth, cellHeight int) (GridGeometry, error) {
	if _, _, err := raster.BufferSize(pixelWidth, pixelHeight, 2); err != nil {
		return GridGeometry{}, err
	}
	if cellWidth <= 0 || cellHeight <= 0 {
		return GridGeometry{}, fmt.Errorf("wayland: invalid cell size %dx%d", cellWidth, cellHeight)
	}
	cols, rows := pixelWidth/cellWidth, pixelHeight/cellHeight
	if cols < effect.MinimumCols || rows < effect.MinimumRows {
		return GridGeometry{}, fmt.Errorf("wayland: output grid %dx%d is below the effect minimum %dx%d", cols, rows, effect.MinimumCols, effect.MinimumRows)
	}
	if rows > cell.MaxGridCells || cols > cell.MaxGridCells/rows {
		return GridGeometry{}, fmt.Errorf("wayland: output grid %dx%d exceeds %d cells", cols, rows, cell.MaxGridCells)
	}
	return GridGeometry{
		PixelWidth:  pixelWidth,
		PixelHeight: pixelHeight,
		CellWidth:   cellWidth,
		CellHeight:  cellHeight,
		Cols:        cols,
		Rows:        rows,
		Right:       pixelWidth - cols*cellWidth,
		Bottom:      pixelHeight - rows*cellHeight,
	}, nil
}

func gridMatchesGeometry(cols, rows int, geometry GridGeometry) bool {
	return cols == geometry.Cols && rows == geometry.Rows
}
