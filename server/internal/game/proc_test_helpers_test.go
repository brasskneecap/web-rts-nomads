package game

import "testing"

// firstProcFor returns the first proc on def matching trigger, failing the test
// if none exists. (Item procs cast abilities; callers assert proc.Ability.)
func firstProcFor(t *testing.T, def *ItemDef, trigger ItemProcTrigger) ItemProc {
	t.Helper()
	for _, p := range def.Procs {
		if p.Trigger == trigger {
			return p
		}
	}
	t.Fatalf("item %q has no %s proc", def.ID, trigger)
	return ItemProc{}
}
