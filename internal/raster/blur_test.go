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
		bw, bh := b.Size()
		want := make([]byte, bw*bh*4)
		for y := 0; y < bh; y++ {
			for x := 0; x < bw; x++ {
				for channel := 0; channel < 3; channel++ {
					sum, count := 0, 0
					for sy := y * 4; sy < min((y+1)*4, h); sy++ {
						for sx := x * 4; sx < min((x+1)*4, w); sx++ {
							sum += int(src[sy*stride+sx*4+channel])
							count++
						}
					}
					want[(y*bw+x)*4+channel] = byte(sum / count)
				}
				want[(y*bw+x)*4+3] = 255
			}
		}
		k := max(1, radius/4)
		for pass := 0; pass < 6; pass++ {
			next := make([]byte, len(want))
			for y := 0; y < bh; y++ {
				for x := 0; x < bw; x++ {
					for channel := 0; channel < 3; channel++ {
						sum := 0
						for offset := -k; offset <= k; offset++ {
							sx, sy := x, y
							if pass%2 == 0 {
								sx = min(max(x+offset, 0), bw-1)
							} else {
								sy = min(max(y+offset, 0), bh-1)
							}
							sum += int(want[(sy*bw+sx)*4+channel])
						}
						next[(y*bw+x)*4+channel] = byte(sum / (2*k + 1))
					}
					next[(y*bw+x)*4+3] = 255
				}
			}
			want = next
		}
		got := make([]byte, len(want))
		if err := b.Draw(got, src, stride); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("radius %d differs from direct averaging", radius)
		}
	}
}
