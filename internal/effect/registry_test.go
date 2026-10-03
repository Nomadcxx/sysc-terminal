package effect

import (
	"testing"

	"github.com/Nomadcxx/sysc-Go/animations"
)

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
