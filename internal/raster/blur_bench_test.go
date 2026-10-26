package raster

import (
	"strconv"
	"testing"
)

// benchFrame builds a full-resolution source frame at the mode's size. The
// content is a slow gradient so the sliding sums behave like a real wallpaper
// rather than a branch-predictable constant.
func benchFrame(w, h, stride int) []byte {
	src := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*stride + x*4
			src[i] = byte(x * 3)
			src[i+1] = byte(y * 5)
			src[i+2] = byte(x + y)
			src[i+3] = 255
		}
	}
	return src
}

// BenchmarkBlurDraw is the end-to-end kernel cost the frame budget cares about:
// downsample, the six sliding passes, and the copy back out.
func BenchmarkBlurDraw(b *testing.B) {
	const w, h = 3440, 1440
	stride := w * 4
	src := benchFrame(w, h, stride)
	for _, radius := range []int{16, 32, 64} {
		b.Run("radius"+strconv.Itoa(radius), func(b *testing.B) {
			bl, err := NewBlur(w, h, radius)
			if err != nil {
				b.Fatal(err)
			}
			dst := make([]byte, bl.smallBytes())
			b.SetBytes(int64(w * h * 4))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := bl.Draw(dst, src, stride); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkBoxBlur isolates the six sliding passes, which are the dominant
// share of Blur.Draw once the downsample is paid for.
func BenchmarkBoxBlur(b *testing.B) {
	const w, h = 860, 360 // 3440x1440 reduced by blurDownsample
	for _, radius := range []int{16, 32, 64} {
		bl, err := NewBlur(w*blurDownsample, h*blurDownsample, radius)
		if err != nil {
			b.Fatal(err)
		}
		src := make([]uint32, w*h)
		dst := make([]uint32, w*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				src[y*w+x] = uint32(byte(x*3)) | uint32(byte(y*5))<<8 | uint32(byte(x+y))<<16 | 0xff000000
			}
		}
		for _, horizontal := range []bool{true, false} {
			name := "vertical"
			if horizontal {
				name = "horizontal"
			}
			b.Run(name+"radius"+strconv.Itoa(radius), func(b *testing.B) {
				b.SetBytes(int64(w * h * 4))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					bl.boxBlur(dst, src, horizontal)
				}
			})
		}
	}
}
