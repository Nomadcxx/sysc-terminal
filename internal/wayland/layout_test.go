package wayland

import "testing"

func TestPixelSizeUsesFractionalScale(t *testing.T) {
	got, err := PixelSize(12, 150)
	if err != nil {
		t.Fatalf("pixel size: %v", err)
	}
	if got != 15 {
		t.Fatalf("pixel size = %d, want 15", got)
	}
}

func TestConfiguredGridUsesDevicePixelsAtFractionalScale(t *testing.T) {
	g, err := ConfiguredGrid(1536, 864, 150, 9, 18)
	if err != nil {
		t.Fatalf("configured grid: %v", err)
	}
	if g.PixelWidth != 1920 || g.PixelHeight != 1080 || g.Cols != 213 || g.Rows != 60 || g.Right != 3 || g.Bottom != 0 {
		t.Fatalf("configured geometry = %+v", g)
	}
}

func TestDeriveGridUsesDevicePixelsAndLetterboxesRemainder(t *testing.T) {
	g, err := DeriveGrid(3440, 1440, 7, 16)
	if err != nil {
		t.Fatalf("derive grid: %v", err)
	}
	if g.Cols != 491 || g.Rows != 90 || g.Right != 3 || g.Bottom != 0 {
		t.Fatalf("geometry = %+v", g)
	}
}

func TestDeriveGridRefusesEffectFloor(t *testing.T) {
	if _, err := DeriveGrid(128, 128, 7, 16); err == nil {
		t.Fatal("grid below 21x24 was accepted")
	}
}

func TestDeriveGridRejectsOversizedBuffer(t *testing.T) {
	if _, err := DeriveGrid(1<<29, 1, 1, 1); err == nil {
		t.Fatal("int32-overflowing shm geometry was accepted")
	}
}

func TestPaintWaitsForConfiguredGrid(t *testing.T) {
	geometry := GridGeometry{Cols: 491, Rows: 90}
	if gridMatchesGeometry(21, 24, geometry) {
		t.Fatal("initial minimum grid can paint before the configured resize arrives")
	}
	if !gridMatchesGeometry(491, 90, geometry) {
		t.Fatal("resized grid does not match configured geometry")
	}
}
