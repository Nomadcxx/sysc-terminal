package effect

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-Go/animations"
)

func TestGreeterThemeCatalogAndEffects(t *testing.T) {
	for _, theme := range []string{"ayu", "amber", "blue", "purple", "green", "orange"} {
		t.Run(theme, func(t *testing.T) {
			if !strings.Contains(List(), "theme  "+theme+" \n") {
				t.Errorf("theme missing from --list")
			}
			for _, id := range animations.GetEffectNames() {
				t.Run(id, func(t *testing.T) {
					e, err := New(id, theme, 80, 24, "SYSC")
					if err != nil {
						t.Fatal(err)
					}
					if err := e.Tick(); err != nil {
						t.Fatal(err)
					}
					if e.Grid() == nil || e.Generation() != 1 {
						t.Fatal("effect did not render")
					}
				})
			}
		})
	}
}

func TestRegistryTickOnce(t *testing.T) {
	for _, id := range animations.GetEffectNames() {
		t.Run(id, func(t *testing.T) {
			e, err := New(id, "nord", 80, 24, "SYSC")
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			e.Tick()
			if e.Grid() == nil {
				t.Fatal("nil grid after tick")
			}
			if e.Generation() != 1 {
				t.Fatalf("generation %d", e.Generation())
			}
		})
	}
}
