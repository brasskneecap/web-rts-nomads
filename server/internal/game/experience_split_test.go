package game

import (
	"testing"
)

func TestExperienceTuning_DefaultsLoaded(t *testing.T) {
	et := gameplayTuning().Experience
	if et.Mode != experienceModeClassic {
		t.Errorf("default experience.mode = %q, want %q", et.Mode, experienceModeClassic)
	}
	if et.SplitDefaultXP != 10 {
		t.Errorf("default experience.splitDefaultXP = %d, want 10", et.SplitDefaultXP)
	}
	if et.SplitEligibilityRadius != 500 {
		t.Errorf("default experience.splitEligibilityRadius = %v, want 500", et.SplitEligibilityRadius)
	}
}
