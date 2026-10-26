package raster

import (
	"bytes"
	"testing"
)

func TestWallpaperBlur(t *testing.T) {
	const w, h, stride = 17, 9, 17*4 + 8
	src := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*stride + x*4
			src[i], src[i+1], src[i+2], src[i+3] = 32, 64, 96, 255
		}
	}
	before := bytes.Clone(src)
	dst := bytes.Repeat([]byte{0xcc}, len(src))
	b, err := NewBlur(w, h, 16)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Draw(dst, src, stride); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src, before) {
		t.Fatal("blur mutated unblurred history")
	}
	bw, bh := b.Size()
	for i := 0; i < bw*bh*4; i += 4 {
		if !bytes.Equal(dst[i:i+4], []byte{32, 64, 96, 255}) {
			t.Fatalf("constant changed: %v", dst[i:i+4])
		}
	}
	// A bright left edge must soften nearby pixels without wrapping to the right.
	clear(src)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src[y*stride+x*4+3] = 255
		}
		src[y*stride] = 255
	}
	b, err = NewBlur(w, h, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Draw(dst, src, stride); err != nil {
		t.Fatal(err)
	}
	if dst[0] == 0 || dst[4] == 0 || dst[(bw-1)*4] != 0 {
		t.Fatalf("edge not softened or wrapped: %v", dst[:w*4])
	}
	first := bytes.Clone(dst)
	if err = b.Draw(dst, src, stride); err != nil || !bytes.Equal(dst, first) {
		t.Fatal("blur accumulated across draws")
	}
	if err = b.Draw(dst[:4], src, stride); err == nil {
		t.Fatal("short buffer accepted")
	}
	for _, radius := range []int{-1, 65} {
		if _, err = NewBlur(w, h, radius); err == nil {
			t.Fatalf("radius %d accepted", radius)
		}
	}
}

// referenceBlur reproduces the kernel the slow way: average every downsample
// block, then run the clamped box passes with a direct neighbourhood sum and no
// sliding-window shortcut. It exists so the optimised path can be held to a
// byte-for-byte match across line lengths that exercise every edge case.
func referenceBlur(src []byte, stride, w, h, radius int) ([]byte, int, int) {
	ds := (w + blurDownsample - 1) / blurDownsample
	dh := (h + blurDownsample - 1) / blurDownsample
	small := make([]byte, ds*dh*4)
	for y := 0; y < dh; y++ {
		for x := 0; x < ds; x++ {
			for channel := 0; channel < 3; channel++ {
				sum, count := 0, 0
				for sy := y * blurDownsample; sy < min((y+1)*blurDownsample, h); sy++ {
					for sx := x * blurDownsample; sx < min((x+1)*blurDownsample, w); sx++ {
						sum += int(src[sy*stride+sx*4+channel])
						count++
					}
				}
				small[(y*ds+x)*4+channel] = byte(sum / count)
			}
			small[(y*ds+x)*4+3] = 255
		}
	}
	k := max(1, radius/blurDownsample)
	for pass := 0; pass < 6; pass++ {
		next := make([]byte, len(small))
		for y := 0; y < dh; y++ {
			for x := 0; x < ds; x++ {
				for channel := 0; channel < 3; channel++ {
					sum := 0
					for offset := -k; offset <= k; offset++ {
						sx, sy := x, y
						if pass%2 == 0 {
							sx = min(max(x+offset, 0), ds-1)
						} else {
							sy = min(max(y+offset, 0), dh-1)
						}
						sum += int(small[(sy*ds+sx)*4+channel])
					}
					next[(y*ds+x)*4+channel] = byte(sum / (2*k + 1))
				}
				next[(y*ds+x)*4+3] = 255
			}
		}
		small = next
	}
	return small, ds, dh
}

// TestWallpaperBlurEdgeCases pins the rewritten kernel to the direct average at
// the line lengths where the left edge, the clamp-free interior and the right
// edge meet. A box of half-width k needs a reduced line of 2k+1 to fit at all,
// so the sizes below walk n from well under that to well over it.
func TestWallpaperBlurEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		w, h   int
		radius int
	}{
		{17, 9, 16},   // reduced line shorter than the box
		{217, 97, 4},  // k=1 with a wide interior
		{217, 97, 16}, // k=4 with a wide interior
		{217, 97, 64}, // k=16 with a wide interior
		{32, 32, 16},  // reduced n=8, one short of 2k+1
		{36, 36, 16},  // reduced n=9, exactly 2k+1
		{40, 40, 16},  // reduced n=10, one past 2k+1
		{217, 4, 64},  // one reduced row
		{4, 217, 64},  // one reduced column
		{1, 1, 1},     // single pixel
	} {
		// The extra bytes keep the source stride wider than the visible row, the
		// way a compositor buffer is padded.
		stride := tc.w*4 + 8
		src := make([]byte, stride*tc.h)
		for y := 0; y < tc.h; y++ {
			for x := 0; x < tc.w; x++ {
				i := y*stride + x*4
				src[i] = byte(x*13 + y*71)
				src[i+1] = byte(x*53 + y*19)
				src[i+2] = byte(x*7 + y*37)
				src[i+3] = 255
			}
		}
		want, w, h := referenceBlur(src, stride, tc.w, tc.h, tc.radius)
		b, err := NewBlur(tc.w, tc.h, tc.radius)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(want))
		if err := b.Draw(got, src, stride); err != nil {
			t.Fatal(err)
		}
		if bw, bh := b.Size(); bw != w || bh != h {
			t.Fatalf("%dx%d radius %d: size %dx%d, want %dx%d", tc.w, tc.h, tc.radius, bw, bh, w, h)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%dx%d radius %d: byte %d is %d, want %d", tc.w, tc.h, tc.radius, i, got[i], want[i])
			}
		}
	}
}

// Compare the packed area sums and sliding passes with direct RGB averaging.
func TestWallpaperBlurMatchesReference(t *testing.T) {
	const w, h, stride = 19, 13, 19*4 + 8
	src := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*stride + x*4
			src[i], src[i+1], src[i+2], src[i+3] = byte(x*13+y*71), byte(x*53+y*19), byte(x*7+y*37), 255
		}
	}
	for _, radius := range []int{1, 16, 64} {
		b, err := NewBlur(w, h, radius)
		if err != nil {
			t.Fatal(err)
		}
		want, _, _ := referenceBlur(src, stride, w, h, radius)
		got := make([]byte, len(want))
		if err := b.Draw(got, src, stride); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("radius %d differs from direct averaging", radius)
		}
	}
}
