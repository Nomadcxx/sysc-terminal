package raster

import (
	"encoding/binary"
	"fmt"
	"image"
)

const MaxBlurRadius = 64
const blurDownsample = 4

// Blur owns reusable reduced pixels. The source remains unblurred so dirty
// cell rendering never inherits a previously softened frame.
type Blur struct {
	width, height, radius int
	small, scratch        *image.RGBA
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
		rect := image.Rect(0, 0, (width+blurDownsample-1)/blurDownsample, (height+blurDownsample-1)/blurDownsample)
		b.small, b.scratch = image.NewRGBA(rect), image.NewRGBA(rect)
	}
	return b, nil
}

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
	w, h := b.small.Rect.Dx(), b.small.Rect.Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var rb, green uint64
			n := uint64(0)
			sx, sy := x*blurDownsample, y*blurDownsample
			if sx+4 <= b.width && sy+4 <= b.height {
				// Two packed pixels per load; 16-bit lanes hold at most 8*255.
				for row := sy; row < sy+4; row++ {
					i := row*stride + sx*4
					a := binary.LittleEndian.Uint64(src[i:])
					c := binary.LittleEndian.Uint64(src[i+8:])
					rb += (a & 0x00ff00ff00ff00ff) + (c & 0x00ff00ff00ff00ff)
					green += ((a >> 8) & 0x000000ff000000ff) + ((c >> 8) & 0x000000ff000000ff)
				}
				rb = (rb & 65535) + ((rb >> 32) & 65535) | (((rb>>16)&65535)+((rb>>48)&65535))<<16
				green = (green & 65535) + ((green >> 32) & 65535)
				n = 16
			} else {
				for row := sy; row < min(sy+4, b.height); row++ {
					for col := sx; col < min(sx+4, b.width); col++ {
						v := binary.LittleEndian.Uint32(src[row*stride+col*4:])
						rb += uint64(v & 0x00ff00ff)
						green += uint64((v >> 8) & 255)
						n++
					}
				}
			}
			i := y*b.small.Stride + x*4
			binary.LittleEndian.PutUint32(b.small.Pix[i:], uint32((rb&65535)/n|(green/n)<<8|((rb>>16)/n)<<16|0xff000000))
		}
	}
	// ponytail: quarter-size pixels trade fine detail for bounded CPU and memory;
	// GPU post-processing is the upgrade if frame-budget measurements require it.
	k := max(1, b.radius/blurDownsample)
	for pass := 0; pass < 3; pass++ {
		boxBlur(b.scratch, b.small, k, true)
		boxBlur(b.small, b.scratch, k, false)
	}
	copy(dst, b.small.Pix)
	return nil
}

// Size is the reduced buffer extent; wp_viewport enlarges it on the compositor.
func (b *Blur) Size() (int, int) {
	if b.radius == 0 {
		return b.width, b.height
	}
	return b.small.Rect.Dx(), b.small.Rect.Dy()
}

// Sliding sums keep each pass linear in pixel count, independent of radius.
func boxBlur(dst, src *image.RGBA, k int, horizontal bool) {
	n, lines, step, advance := src.Rect.Dy(), src.Rect.Dx(), src.Stride, 4
	if horizontal {
		n, lines, step, advance = src.Rect.Dx(), src.Rect.Dy(), 4, src.Stride
	}
	divisor := 2*k + 1
	for line := 0; line < lines; line++ {
		start := line * advance
		var blue, green, red int
		for j := -k; j <= k; j++ {
			i := start + min(max(j, 0), n-1)*step
			v := binary.LittleEndian.Uint32(src.Pix[i:])
			blue += int(v & 255)
			green += int((v >> 8) & 255)
			red += int((v >> 16) & 255)
		}
		for pos := 0; pos < n; pos++ {
			i := start + pos*step
			binary.LittleEndian.PutUint32(dst.Pix[i:], uint32(blue/divisor)|uint32(green/divisor)<<8|uint32(red/divisor)<<16|0xff000000)
			left := binary.LittleEndian.Uint32(src.Pix[start+max(pos-k, 0)*step:])
			right := binary.LittleEndian.Uint32(src.Pix[start+min(pos+k+1, n-1)*step:])
			blue += int(right&255) - int(left&255)
			green += int((right>>8)&255) - int((left>>8)&255)
			red += int((right>>16)&255) - int((left>>16)&255)
		}
	}
}

func (b *Blur) smallBytes() int { w, h := b.Size(); return w * h * 4 }
