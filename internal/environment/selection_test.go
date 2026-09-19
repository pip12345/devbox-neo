package environment

import "testing"

func TestCompoundSlotSelectorsAreExact(t *testing.T) {
	work := t.TempDir()
	for _, selector := range []string{".project", ".profile-work", ".profile-work.project"} {
		id, err := IdentifySlot(work, selector)
		if err != nil || id.Selector() != selector || id.ValidateSlot() != nil {
			t.Fatal(id, err)
		}
	}
	for _, selector := range []string{"work", ".profile:work", ".profile:work.project", ".profile-work.project.project"} {
		if _, err := IdentifySlot(work, selector); err == nil {
			t.Fatal("accepted ambiguous/alternate selector", selector)
		}
	}
}
