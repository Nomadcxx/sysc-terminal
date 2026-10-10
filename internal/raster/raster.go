package raster

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"log/slog"
	"math"
	"os"
	"sort"
	"sync"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"
)

const rasterCacheMax = 256

// ponytail: cap font reads at 32 MiB; larger fonts need a streaming loader and budget review.
const maxFontFileBytes = 32 << 20

// src is the face that drew the glyph; the same GID number means different
// outlines in different faces, so the cache key must carry the face index.
type rasterKey struct {
	gid  font.GID
	ppem uint16
	src  uint8
}

// Rasterizer draws cells with the primary face and, per rune, falls back to
// at most rasterFallbackMax extra faces when the primary lacks a glyph.
// Cell metrics always come from the primary face, so a fallback can never
// reflow the grid.
type Rasterizer struct {
	font  *font.Font // primary face source, == fonts[0]
	fonts []*font.Font
	faces []*font.Face // one per font at the current ppem

	ppem  uint16
	scale float32
	cellW int
	cellH int
	base  int
	cache map[rasterKey]*image.Alpha
	order []rasterKey
	black []byte
	tiles map[cell.Cell][]byte
	// blocks holds the geometric block-element masks for the current size.
	blocks map[rune]*image.Alpha

	faceOf   map[rune]uint8 // resolved face per rune
	affected map[rune]uint8 // runes that needed a fallback face
}

// rasterFallbackMax bounds the face set (primary + fallbacks);
// affectedRunesMax bounds the diagnostic record.
const (
	rasterFallbackMax = 2
	affectedRunesMax  = 256
)

func BufferSize(width, height, slots int) (stride, total int, err error) {
	if width <= 0 || height <= 0 || slots <= 0 {
		return 0, 0, fmt.Errorf("raster: size %dx%d slots %d", width, height, slots)
	}
	if width > math.MaxInt32/4 || height > math.MaxInt32 || slots > math.MaxInt32 {
		return 0, 0, fmt.Errorf("raster: %dx%d slots %d exceed int32 buffer limits", width, height, slots)
	}
	s := int64(width) * 4
	perSlot := s * int64(height)
	if perSlot > math.MaxInt32 || int64(slots) > math.MaxInt32/perSlot {
		return 0, 0, fmt.Errorf("raster: %dx%d slots %d exceed int32 buffer limits", width, height, slots)
	}
	tot := perSlot * int64(slots)
	return int(s), int(tot), nil
}

// Open loads path and quietly attaches up to rasterFallbackMax other
// installed faces as per-glyph fallbacks.
func Open(path string, pixelSize int) (*Rasterizer, error) {
	return OpenWithFallbacks(append([]string{path}, fallbackFontPaths(path)...), pixelSize)
}

// OpenWithFallbacks builds a rasterizer from an ordered face chain; the
// first path is the primary. The primary must parse; a fallback that fails
// to load is skipped, because a broken file under a system font directory
// must not stop the terminal.
func OpenWithFallbacks(paths []string, pixelSize int) (*Rasterizer, error) {
	var fonts []*font.Font
	for _, path := range paths {
		if len(fonts) > rasterFallbackMax {
			break
		}
		parsed, err := loadFont(path)
		if err != nil {
			if len(fonts) == 0 {
				return nil, err
			}
			continue
		}
		fonts = append(fonts, parsed)
	}
	if len(fonts) == 0 {
		return nil, fmt.Errorf("raster: no usable font")
	}
	r := &Rasterizer{
		font:   fonts[0],
		fonts:  fonts,
		cache:  make(map[rasterKey]*image.Alpha),
		tiles:  make(map[cell.Cell][]byte),
		faceOf: make(map[rune]uint8),
	}
	if err := r.SetPixelSize(pixelSize); err != nil {
		return nil, err
	}
	return r, nil
}

func loadFont(path string) (*font.Font, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("raster: font is not a regular file")
	}
	if info.Size() > maxFontFileBytes {
		return nil, fmt.Errorf("raster: font exceeds %d MiB limit", maxFontFileBytes>>20)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFontFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxFontFileBytes {
		return nil, fmt.Errorf("raster: font exceeds %d MiB limit", maxFontFileBytes>>20)
	}
	loader, err := ot.NewLoader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("raster: load font: %w", err)
	}
	parsed, err := font.NewFont(loader)
	if err != nil {
		return nil, fmt.Errorf("raster: parse font: %w", err)
	}
	return parsed, nil
}

func (r *Rasterizer) SetPixelSize(pixelSize int) error {
	if r == nil || r.font == nil {
		return fmt.Errorf("raster: nil font")
	}
	if pixelSize < 1 {
		pixelSize = 1
	}
	if pixelSize > math.MaxUint16 {
		return fmt.Errorf("raster: font pixel size %d exceeds uint16", pixelSize)
	}
	ppem := uint16(pixelSize)
	if r.ppem == ppem {
		return nil
	}
	r.faces = r.faces[:0]
	for _, f := range r.fonts {
		face := font.NewFace(f)
		face.SetPpem(ppem, ppem)
		r.faces = append(r.faces, face)
	}
	r.ppem = ppem
	r.scale = float32(pixelSize) / float32(r.font.Upem())
	// Metrics always come from the primary face; a grid that reflows between
	// glyphs is worse than one that clips a wide fallback glyph (blit clips).
	r.cellW, r.cellH, r.base = metrics(r.faces[0], r.scale, pixelSize)
	r.cache = make(map[rasterKey]*image.Alpha)
	r.order = nil
	r.tiles = make(map[cell.Cell][]byte)
	r.blocks = nil
	r.faceOf = make(map[rune]uint8)
	r.affected = make(map[rune]uint8)
	return nil
}

func (r *Rasterizer) CellSize() (w, h int) {
	if r == nil {
		return 0, 0
	}
	return r.cellW, r.cellH
}

func (r *Rasterizer) Draw(g *cell.Grid, dst []byte, width, height, stride int) error {
	return r.DrawChanged(g, nil, dst, width, height, stride)
}

// DrawChanged redraws only cells that differ from the grid previously drawn
// into this buffer. Each Wayland shm slot keeps its own prior grid because the
// compositor releases alternating buffers with different frame contents.
func (r *Rasterizer) DrawChanged(g, previous *cell.Grid, dst []byte, width, height, stride int) error {
	if r == nil || g == nil {
		return fmt.Errorf("raster: nil")
	}
	if width <= 0 || height <= 0 || stride <= 0 || width > int(^uint(0)>>1)/4 || stride < width*4 || height > int(^uint(0)>>1)/stride || len(dst) < stride*height {
		return fmt.Errorf("raster: buffer %d stride %d for %dx%d", len(dst), stride, width, height)
	}
	full := previous == nil || previous.Cols != g.Cols || previous.Rows != g.Rows
	if full {
		r.fillBlack(dst, stride, width, height)
	}
	for y := 0; y < g.Rows; y++ {
		dy := y * r.cellH
		if dy >= height {
			break
		}
		for x := 0; x < g.Cols; x++ {
			dx := x * r.cellW
			if dx >= width {
				break
			}
			c := g.At(x, y)
			if !full && c == previous.At(x, y) {
				continue
			}
			if !r.wide(g, x, y) && (full || !r.wide(previous, x, y)) && c.Ch != 0 {
				if tile := r.tile(c); tile != nil {
					rowBytes := min(r.cellW, width-dx) * 4
					for row := 0; row < min(r.cellH, height-dy); row++ {
						copy(dst[(dy+row)*stride+dx*4:][:rowBytes], tile[row*r.cellW*4:][:rowBytes])
					}
					continue
				}
			}
			if !full {
				clearWidth := r.cellW
				if r.wide(g, x, y) || r.wide(previous, x, y) {
					clearWidth = 2 * r.cellW
				}
				clearOpaqueBlack(dst, stride, dx, dy, clearWidth, r.cellH, width, height)
			}
			r.drawCell(g, x, y, c, dst, width, height, stride)
		}
	}
	return nil
}

func (r *Rasterizer) wide(g *cell.Grid, x, y int) bool {
	return x+1 < g.Cols && g.At(x+1, y).Ch == 0
}

func (r *Rasterizer) drawCell(g *cell.Grid, x, y int, c cell.Cell, dst []byte, width, height, stride int) {
	if c.Ch == 0 {
		return
	}
	r.drawGlyphCell(c, r.wide(g, x, y), x*r.cellW, y*r.cellH, dst, width, height, stride)
}

func (r *Rasterizer) drawGlyphCell(c cell.Cell, wide bool, dx, dy int, dst []byte, width, height, stride int) {
	cellWidth, clipWidth := r.cellW, r.cellW
	if wide {
		cellWidth, clipWidth = 2*r.cellW, 2*r.cellW
	}
	bg := c.Bg
	if bg.A == 0 {
		bg = color.RGBA{A: 255}
	}
	// ponytail: the canvas starts opaque black, so leave transparent/default and explicit black cells alone.
	if bg.R != 0 || bg.G != 0 || bg.B != 0 || bg.A != 255 {
		cellFill(dst, stride, dx, dy, cellWidth, r.cellH, width, height, bg)
	}
	if c.Ch == ' ' {
		return
	}
	mask := r.mask(c.Ch)
	blit(dst, stride, dx, dy, width, height, mask, c.Fg, clipWidth)
}

func metrics(face *font.Face, scale float32, pixelSize int) (cellW, cellH, base int) {
	gid, ok := face.NominalGlyph('M')
	adv := float32(pixelSize)
	if ok {
		adv = face.HorizontalAdvance(gid) * scale
	}
	cellW = int(math.Round(float64(adv)))
	if cellW < 1 {
		cellW = 1
	}
	ext, ok := face.FontHExtents()
	line := float32(pixelSize) * 1.2
	if ok {
		line = (ext.Ascender - ext.Descender + ext.LineGap) * scale
		base = int(math.Round(float64(ext.Ascender * scale)))
	}
	cellH = int(math.Round(float64(line)))
	if cellH < 1 {
		cellH = 1
	}
	if base < 1 {
		base = cellH * 3 / 4
	}
	return cellW, cellH, base
}

// faceFor resolves ch to the first face that draws it, preferring the
// primary, and caches the answer. A rune that no face draws resolves to
// the primary's glyph 0 (the .notdef path) exactly as before. Runes served
// by a fallback face are recorded, bounded, for diagnostics: the effect
// author can see which glyphs leave the primary face.
func (r *Rasterizer) faceFor(ch rune) (*font.Face, uint8) {
	if src, ok := r.faceOf[ch]; ok {
		return r.faces[src], src
	}
	src := uint8(0)
	for i, face := range r.faces {
		if _, ok := face.NominalGlyph(ch); ok {
			src = uint8(i)
			break
		}
	}
	if src > 0 {
		if len(r.affected) < affectedRunesMax {
			r.affected[ch] = src
		} else {
			affectedFullOnce.Do(func() {
				slog.Warn("raster: fallback rune record is full; later fallback runes are not counted")
			})
		}
	}
	r.faceOf[ch] = src
	return r.faces[src], src
}

// AffectedRunes lists the runes drawn through a fallback face so far, as
// sorted 'U+XXXX' strings. Bounded; cleared when the pixel size changes.
func (r *Rasterizer) AffectedRunes() []string {
	out := make([]string, 0, len(r.affected))
	for ch := range r.affected {
		out = append(out, fmt.Sprintf("%U", ch))
	}
	sort.Strings(out)
	return out
}

func (r *Rasterizer) mask(ch rune) *image.Alpha {
	if m := r.blockMask(ch); m != nil {
		return m
	}
	face, src := r.faceFor(ch)
	gid, _ := face.NominalGlyph(ch)
	key := rasterKey{gid: gid, ppem: r.ppem, src: src}
	if m, hit := r.cache[key]; hit {
		return m
	}
	m := r.raster(face, gid)
	if len(r.order) >= rasterCacheMax {
		drop := len(r.order) / 2
		for _, old := range r.order[:drop] {
			delete(r.cache, old)
		}
		r.order = r.order[drop:]
	}
	r.cache[key] = m
	r.order = append(r.order, key)
	return m
}

func (r *Rasterizer) raster(face *font.Face, gid font.GID) *image.Alpha {
	glyphWidth := int(math.Round(float64(face.HorizontalAdvance(gid) * r.scale)))
	glyphWidth = max(r.cellW, min(glyphWidth, 2*r.cellW))
	mask := image.NewAlpha(image.Rect(0, 0, glyphWidth, r.cellH))
	outline, ok := face.GlyphDataOutline(gid)
	if !ok || len(outline.Segments) == 0 {
		return image.NewAlpha(image.Rectangle{})
	}
	rast := vector.NewRasterizer(glyphWidth, r.cellH)
	addOutline(rast, outline, 0, float32(r.base), r.scale)
	rast.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	ink := image.Rectangle{Min: image.Pt(r.cellW, r.cellH), Max: image.Point{}}
	for y := 0; y < r.cellH; y++ {
		row := y * mask.Stride
		for x := 0; x < glyphWidth; x++ {
			if mask.Pix[row+x] == 0 {
				continue
			}
			ink.Min.X = min(ink.Min.X, x)
			ink.Min.Y = min(ink.Min.Y, y)
			ink.Max.X = max(ink.Max.X, x+1)
			ink.Max.Y = max(ink.Max.Y, y+1)
		}
	}
	if ink.Empty() {
		return image.NewAlpha(image.Rectangle{})
	}
	return mask.SubImage(ink).(*image.Alpha)
}

func addOutline(rast *vector.Rasterizer, outline font.GlyphOutline, originX, originY, scale float32) {
	px := func(p ot.SegmentPoint) (float32, float32) {
		return originX + p.X*scale, originY - p.Y*scale
	}
	for _, seg := range outline.Segments {
		switch seg.Op {
		case ot.SegmentOpMoveTo:
			rast.MoveTo(px(seg.Args[0]))
		case ot.SegmentOpLineTo:
			rast.LineTo(px(seg.Args[0]))
		case ot.SegmentOpQuadTo:
			x1, y1 := px(seg.Args[0])
			x2, y2 := px(seg.Args[1])
			rast.QuadTo(x1, y1, x2, y2)
		case ot.SegmentOpCubeTo:
			x1, y1 := px(seg.Args[0])
			x2, y2 := px(seg.Args[1])
			x3, y3 := px(seg.Args[2])
			rast.CubeTo(x1, y1, x2, y2, x3, y3)
		}
	}
	rast.ClosePath()
}

func (r *Rasterizer) fillBlack(dst []byte, stride, width, height int) {
	rowBytes := width * 4
	if len(r.black) < rowBytes {
		r.black = make([]byte, rowBytes)
		for i := 3; i < rowBytes; i += 4 {
			r.black[i] = 255
		}
	}
	for y := 0; y < height; y++ {
		row := dst[y*stride : y*stride+rowBytes]
		copy(row, r.black[:rowBytes])
	}
}

func clearOpaqueBlack(dst []byte, stride, x0, y0, cellW, cellH, width, height int) {
	x1 := min(x0+cellW, width)
	y1 := min(y0+cellH, height)
	start := x0 * 4
	end := x1 * 4
	for y := y0; y < y1; y++ {
		row := dst[y*stride : y*stride+stride]
		clear(row[start:end])
		for alpha := start + 3; alpha < end; alpha += 4 {
			row[alpha] = 255
		}
	}
}

func cellFill(dst []byte, stride, x0, y0, cw, ch, width, height int, c color.RGBA) {
	b, g, r, a := premul(c)
	x1 := min(x0+cw, width)
	y1 := min(y0+ch, height)
	for y := y0; y < y1; y++ {
		row := y * stride
		for x := x0; x < x1; x++ {
			off := row + x*4
			dst[off+0] = b
			dst[off+1] = g
			dst[off+2] = r
			dst[off+3] = a
		}
	}
}

func blit(dst []byte, stride, x0, y0, width, height int, mask *image.Alpha, fg color.RGBA, clipWidth int) {
	if mask == nil {
		return
	}
	fb, fg2, fr, _ := premul(fg)
	mb := mask.Bounds()
	if mb.Empty() {
		return
	}
	minX := max(mb.Min.X, -x0)
	minY := max(mb.Min.Y, -y0)
	maxX := min(mb.Max.X, min(width-x0, clipWidth))
	maxY := min(mb.Max.Y, height-y0)
	if minX >= maxX || minY >= maxY {
		return
	}
	for my := minY; my < maxY; my++ {
		dy := y0 + my
		row := dy * stride
		maskRow := mask.PixOffset(mb.Min.X, my)
		for mx := minX; mx < maxX; mx++ {
			dx := x0 + mx
			cov := mask.Pix[maskRow+mx-mb.Min.X]
			if cov == 0 {
				continue
			}
			off := row + dx*4
			if cov == 255 {
				dst[off+0], dst[off+1], dst[off+2] = fb, fg2, fr
				continue
			}
			dst[off+0] = mix(dst[off+0], fb, cov)
			dst[off+1] = mix(dst[off+1], fg2, cov)
			dst[off+2] = mix(dst[off+2], fr, cov)
		}
	}
}

func premul(c color.RGBA) (b, g, r, a byte) {
	aa := uint32(c.A)
	return byte(uint32(c.B) * aa / 255), byte(uint32(c.G) * aa / 255), byte(uint32(c.R) * aa / 255), c.A
}

func mix(dst, src, cov byte) byte {
	c := uint32(cov)
	v := uint32(dst)*(255-c) + uint32(src)*c
	return byte((v + 1 + (v >> 8)) >> 8)
}

// ponytail: 256 tiles of at most 4 KiB bound added storage to 1 MiB;
// larger cells and new colours after the cap use the glyph path. A measured
// need for palette turnover can replace this with bounded eviction.
func (r *Rasterizer) tile(c cell.Cell) []byte {
	if r.cellW > 4096/4/r.cellH {
		return nil
	}
	if pixels, ok := r.tiles[c]; ok {
		return pixels
	}
	if len(r.tiles) >= rasterCacheMax {
		return nil
	}
	pixels := make([]byte, r.cellW*r.cellH*4)
	r.fillBlack(pixels, r.cellW*4, r.cellW, r.cellH)
	r.drawGlyphCell(c, false, 0, 0, pixels, r.cellW, r.cellH, r.cellW*4)
	r.tiles[c] = pixels
	return pixels
}

var affectedFullOnce sync.Once
