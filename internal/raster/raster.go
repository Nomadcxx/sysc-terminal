package raster

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"
)

const rasterCacheMax = 256

type rasterKey struct {
	gid  font.GID
	ppem uint16
}

type Rasterizer struct {
	face  *font.Face
	ppem  uint16
	scale float32
	cellW int
	cellH int
	base  int
	cache map[rasterKey]*image.Alpha
	order []rasterKey
}

func BufferSize(width, height, slots int) (stride, total int, err error) {
	if width <= 0 || height <= 0 || slots <= 0 {
		return 0, 0, fmt.Errorf("raster: size %dx%d slots %d", width, height, slots)
	}
	s := int64(width) * 4
	tot := s * int64(height) * int64(slots)
	if s > math.MaxInt32 || tot > math.MaxInt32 {
		return 0, 0, fmt.Errorf("raster: %dx%d needs %d bytes", width, height, tot)
	}
	return int(s), int(tot), nil
}

func Open(path string, pixelSize int) (*Rasterizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if pixelSize < 1 {
		pixelSize = 1
	}
	loader, err := ot.NewLoader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("raster: load font: %w", err)
	}
	parsed, err := font.NewFont(loader)
	if err != nil {
		return nil, fmt.Errorf("raster: parse font: %w", err)
	}
	face := font.NewFace(parsed)
	ppem := uint16(pixelSize)
	face.SetPpem(ppem, ppem)
	scale := float32(pixelSize) / float32(parsed.Upem())
	cellW, cellH, base := metrics(face, scale, pixelSize)
	return &Rasterizer{
		face:  face,
		ppem:  ppem,
		scale: scale,
		cellW: cellW,
		cellH: cellH,
		base:  base,
		cache: make(map[rasterKey]*image.Alpha),
	}, nil
}

func (r *Rasterizer) CellSize() (w, h int) {
	if r == nil {
		return 0, 0
	}
	return r.cellW, r.cellH
}

func (r *Rasterizer) Draw(g *cell.Grid, dst []byte, width, height, stride int) error {
	if r == nil || g == nil {
		return fmt.Errorf("raster: nil")
	}
	if stride < width*4 || len(dst) < stride*height {
		return fmt.Errorf("raster: buffer %d stride %d for %dx%d", len(dst), stride, width, height)
	}
	fill(dst, stride, width, height, color.RGBA{A: 255})
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
			bg := c.Bg
			if bg.A == 0 {
				bg = color.RGBA{A: 255}
			}
			cellFill(dst, stride, dx, dy, r.cellW, r.cellH, width, height, bg)
			if c.Ch == ' ' || c.Ch == 0 {
				continue
			}
			mask := r.mask(c.Ch)
			blit(dst, stride, dx, dy, width, height, mask, c.Fg)
		}
	}
	return nil
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

func (r *Rasterizer) mask(ch rune) *image.Alpha {
	gid, ok := r.face.NominalGlyph(ch)
	if !ok {
		gid = 0
	}
	key := rasterKey{gid: gid, ppem: r.ppem}
	if m, hit := r.cache[key]; hit {
		return m
	}
	m := r.raster(gid)
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

func (r *Rasterizer) raster(gid font.GID) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, r.cellW, r.cellH))
	outline, ok := r.face.GlyphDataOutline(gid)
	if !ok || len(outline.Segments) == 0 {
		return mask
	}
	rast := vector.NewRasterizer(r.cellW, r.cellH)
	addOutline(rast, outline, 0, float32(r.base), r.scale)
	rast.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	return mask
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

func fill(dst []byte, stride, width, height int, c color.RGBA) {
	cellFill(dst, stride, 0, 0, width, height, width, height, c)
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

func blit(dst []byte, stride, x0, y0, width, height int, mask *image.Alpha, fg color.RGBA) {
	if mask == nil {
		return
	}
	mb := mask.Bounds()
	for my := mb.Min.Y; my < mb.Max.Y; my++ {
		dy := y0 + my - mb.Min.Y
		if dy < 0 || dy >= height {
			continue
		}
		row := dy * stride
		for mx := mb.Min.X; mx < mb.Max.X; mx++ {
			dx := x0 + mx - mb.Min.X
			if dx < 0 || dx >= width {
				continue
			}
			cov := mask.AlphaAt(mx, my).A
			if cov == 0 {
				continue
			}
			off := row + dx*4
			fb, fg2, fr, fa := premul(fg)
			dst[off+0] = mix(dst[off+0], fb, cov)
			dst[off+1] = mix(dst[off+1], fg2, cov)
			dst[off+2] = mix(dst[off+2], fr, cov)
			dst[off+3] = mix(dst[off+3], fa, cov)
		}
	}
}

func premul(c color.RGBA) (b, g, r, a byte) {
	aa := uint32(c.A)
	return byte(uint32(c.B) * aa / 255), byte(uint32(c.G) * aa / 255), byte(uint32(c.R) * aa / 255), c.A
}

func mix(dst, src, cov byte) byte {
	c := uint32(cov)
	return byte((uint32(dst)*(255-c) + uint32(src)*c) / 255)
}
