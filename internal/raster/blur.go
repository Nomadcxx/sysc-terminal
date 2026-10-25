package raster

import (
	"encoding/binary"
	"fmt"
)

const MaxBlurRadius = 64
const blurDownsample = 4

// Blur owns reusable reduced pixels. The source remains unblurred so dirty
// cell rendering never inherits a previously softened frame. The reduced
// buffers are words rather than bytes: the sliding passes only ever move whole
// pixels, and a word view removes the pack/unpack and the bounds checks that
// byte indexing costs in the hot loop.
type Blur struct {
	width, height, radius int
	smallW, smallH        int
	small, scratch        []uint32
	// sumB, sumG, sumR hold the running box sums for a whole row of columns
	// during a vertical pass, so that pass walks the image a row at a time.
	sumB, sumG, sumR []uint32
	// quo maps a channel sum to its exact integer quotient by the divisor, so a
	// pass pays one load per channel instead of three integer divisions.
	quo []uint8
}

func NewBlur(width, height, radius int) (*Blur, error) {
	if _, _, err := BufferSize(width, height, 1); err != nil {
		return nil, err
	}
	if radius < 0 || radius > MaxBlurRadius {
		return nil, fmt.Errorf("raster: blur outside 0..%d", MaxBlurRadius)
	}
	b := &Blur{width: width, height: height, radius: radius}
	if radius != 0 {
		b.smallW = (width + blurDownsample - 1) / blurDownsample
		b.smallH = (height + blurDownsample - 1) / blurDownsample
		b.small = make([]uint32, b.smallW*b.smallH)
		b.scratch = make([]uint32, b.smallW*b.smallH)
		b.sumB, b.sumG, b.sumR = make([]uint32, b.smallW), make([]uint32, b.smallW), make([]uint32, b.smallW)
		divisor := 2*b.kernel() + 1
		b.quo = make([]uint8, 255*divisor+1)
		for i := range b.quo {
			b.quo[i] = uint8(i / divisor)
		}
	}
	return b, nil
}

// kernel is the half-width of the box on the reduced image.
func (b *Blur) kernel() int { return max(1, b.radius/blurDownsample) }

func (b *Blur) Draw(dst, src []byte, stride int) error {
	if stride < b.width*4 || stride > len(src)/b.height || len(dst) < b.smallBytes() {
		return fmt.Errorf("raster: short blur buffer or stride")
	}
	if b.radius == 0 {
		for y := 0; y < b.height; y++ {
			copy(dst[y*b.width*4:(y+1)*b.width*4], src[y*stride:y*stride+b.width*4])
		}
		return nil
	}
	// Average every source pixel: point sampling can erase thin ASCII strokes.
	b.downsample(src, stride)
	for pass := 0; pass < 3; pass++ {
		b.boxBlur(b.scratch, b.small, true)
		b.boxBlur(b.small, b.scratch, false)
	}
	small := b.small
	out := dst[:len(small)*4]
	_ = out[len(out)-1]
	for i, v := range small {
		binary.LittleEndian.PutUint32(out[i*4:], v)
	}
	return nil
}

// downsample averages each blurDownsample square of the source into one word.
func (b *Blur) downsample(src []byte, stride int) {
	small, w := b.small, b.smallW
	var rows [blurDownsample][]byte
	for y := 0; y < b.smallH; y++ {
		sy := y * blurDownsample
		nr := min(blurDownsample, b.height-sy)
		for r := 0; r < nr; r++ {
			start := (sy + r) * stride
			rows[r] = src[start : start+stride]
		}
		full := nr == blurDownsample
		for x := 0; x < w; x++ {
			var rb, green uint64
			n := uint64(0)
			sx := x * blurDownsample
			nc := min(blurDownsample, b.width-sx)
			if full && nc == blurDownsample {
				// Two packed pixels per load; 16-bit lanes hold at most 8*255.
				for r := 0; r < blurDownsample; r++ {
					p := rows[r][sx*4 : sx*4+16]
					a := binary.LittleEndian.Uint64(p[:8])
					c := binary.LittleEndian.Uint64(p[8:])
					rb += (a & 0x00ff00ff00ff00ff) + (c & 0x00ff00ff00ff00ff)
					green += ((a >> 8) & 0x000000ff000000ff) + ((c >> 8) & 0x000000ff000000ff)
				}
				rb = (rb & 65535) + ((rb >> 32) & 65535) | (((rb>>16)&65535)+((rb>>48)&65535))<<16
				green = (green & 65535) + ((green >> 32) & 65535)
				// Every block here holds 16 pixels, so the average is a shift.
				small[y*w+x] = uint32((rb&65535)>>4|(green>>4)<<8|((rb>>16)>>4)<<16) | 0xff000000
				continue
			}
			for r := 0; r < nr; r++ {
				row := rows[r]
				for col := 0; col < nc; col++ {
					v := binary.LittleEndian.Uint32(row[(sx+col)*4:])
					rb += uint64(v & 0x00ff00ff)
					green += uint64((v >> 8) & 255)
					n++
				}
			}
			small[y*w+x] = uint32((rb&65535)/n|(green/n)<<8|((rb>>16)/n)<<16) | 0xff000000
		}
	}
}

// Size is the reduced buffer extent; wp_viewport enlarges it on the compositor.
func (b *Blur) Size() (int, int) {
	if b.radius == 0 {
		return b.width, b.height
	}
	return b.smallW, b.smallH
}

// boxBlur slides a box of width 2k+1 along every line of src into dst. Sliding
// sums keep the pass linear in pixel count, independent of radius, and quo turns
// the three per-pixel divisions into three loads. Each line is walked as a left
// edge, a clamp-free interior and a right edge, because the edge clamps are only
// needed where the window actually overhangs the line.
//
// The output is the sum of the clamped window divided by 2k+1, matching a
// direct neighbourhood average.
func (b *Blur) boxBlur(dst, src []uint32, horizontal bool) {
	if horizontal {
		for y := 0; y < b.smallH; y++ {
			off := y * b.smallW
			boxLine(dst[off:off+b.smallW], src[off:off+b.smallW], b.kernel(), b.quo)
		}
		return
	}
	b.boxColumns(dst, src)
}

// boxLine blurs one contiguous line. Every load and store in the interior loop
// indexes a slice cut to exactly the loop's extent, so the compiler can drop
// the bounds checks there.
func boxLine(d, s []uint32, k int, quo []uint8) {
	n := len(s)
	last := n - 1
	first, tail := s[0], s[last]
	var blue, green, red uint32
	for j := -k; j <= k; j++ {
		v := s[min(max(j, 0), last)]
		blue += v & 255
		green += (v >> 8) & 255
		red += (v >> 16) & 255
	}
	// A line shorter than the box is all edge and never reaches the interior.
	leftEnd := min(k, n)
	rightStart := max(n-k-1, 0)
	if rightStart < leftEnd {
		rightStart = leftEnd
	}
	// Left edge: the window leaves the first pixel one copy at a time.
	for pos := 0; pos < leftEnd; pos++ {
		d[pos] = uint32(quo[blue]) | uint32(quo[green])<<8 | uint32(quo[red])<<16 | 0xff000000
		v := s[min(pos+k+1, last)]
		blue += v&255 - first&255
		green += (v>>8)&255 - (first>>8)&255
		red += (v>>16)&255 - (first>>16)&255
	}
	// Interior: both ends of the window move, so there is nothing to clamp.
	if inner := rightStart - leftEnd; inner > 0 {
		dd := d[leftEnd:rightStart]
		add := s[leftEnd+k+1 : rightStart+k+1]
		sub := s[leftEnd-k : rightStart-k]
		for i := 0; i < inner; i++ {
			dd[i] = uint32(quo[blue]) | uint32(quo[green])<<8 | uint32(quo[red])<<16 | 0xff000000
			a, z := add[i], sub[i]
			blue += a&255 - z&255
			green += (a>>8)&255 - (z>>8)&255
			red += (a>>16)&255 - (z>>16)&255
		}
	}
	// Right edge: the window enters the last pixel one copy at a time.
	for pos := rightStart; pos < n; pos++ {
		d[pos] = uint32(quo[blue]) | uint32(quo[green])<<8 | uint32(quo[red])<<16 | 0xff000000
		v := s[max(pos-k, 0)]
		blue += tail&255 - v&255
		green += (tail>>8)&255 - (v>>8)&255
		red += (tail>>16)&255 - (v>>16)&255
	}
}

// boxColumns blurs down each column without walking memory at a stride: the
// running sums for a whole row of columns stay in one buffer, so the pass reads
// and writes the image a row at a time and every index is provably in bounds.
func (b *Blur) boxColumns(dst, src []uint32) {
	w, h, k, quo := b.smallW, b.smallH, b.kernel(), b.quo
	bs, gs, rs := b.sumB, b.sumG, b.sumR
	clear(bs)
	clear(gs)
	clear(rs)
	last := h - 1
	// Hints so the compiler can drop the bounds checks in the loops below.
	_, _, _ = bs[w-1], gs[w-1], rs[w-1]
	for j := -k; j <= k; j++ {
		row := src[min(max(j, 0), last)*w:][:w]
		_ = row[w-1]
		for x := 0; x < w; x++ {
			v := row[x]
			bs[x] += v & 255
			gs[x] += (v >> 8) & 255
			rs[x] += (v >> 16) & 255
		}
	}
	for y := 0; y < h; y++ {
		d := dst[y*w : y*w+w]
		sub := src[min(max(y-k, 0), last)*w:][:w]
		add := src[min(y+k+1, last)*w:][:w]
		_, _, _ = d[w-1], add[w-1], sub[w-1]
		x := 0
		for ; x+1 < w; x += 2 {
			b0, g0, r0 := bs[x], gs[x], rs[x]
			b1, g1, r1 := bs[x+1], gs[x+1], rs[x+1]
			d[x] = uint32(quo[b0]) | uint32(quo[g0])<<8 | uint32(quo[r0])<<16 | 0xff000000
			d[x+1] = uint32(quo[b1]) | uint32(quo[g1])<<8 | uint32(quo[r1])<<16 | 0xff000000
			a0, z0 := add[x], sub[x]
			a1, z1 := add[x+1], sub[x+1]
			bs[x] = b0 + (a0&255 - z0&255)
			gs[x] = g0 + ((a0>>8)&255 - (z0>>8)&255)
			rs[x] = r0 + ((a0>>16)&255 - (z0>>16)&255)
			bs[x+1] = b1 + (a1&255 - z1&255)
			gs[x+1] = g1 + ((a1>>8)&255 - (z1>>8)&255)
			rs[x+1] = r1 + ((a1>>16)&255 - (z1>>16)&255)
		}
		for ; x < w; x++ {
			bsum, gsum, rsum := bs[x], gs[x], rs[x]
			d[x] = uint32(quo[bsum]) | uint32(quo[gsum])<<8 | uint32(quo[rsum])<<16 | 0xff000000
			a, z := add[x], sub[x]
			bs[x] = bsum + (a&255 - z&255)
			gs[x] = gsum + ((a>>8)&255 - (z>>8)&255)
			rs[x] = rsum + ((a>>16)&255 - (z>>16)&255)
		}
	}
}

func (b *Blur) smallBytes() int { return b.smallW * b.smallH * 4 }
