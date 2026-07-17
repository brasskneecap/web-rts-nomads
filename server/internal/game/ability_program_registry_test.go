package game

import "testing"

func TestActionRegistryHasCoreActions(t *testing.T) {
	for _, at := range []ActionType{ActionDealDamage, ActionRestoreHealth, ActionSelectTargets} {
		if _, ok := lookupActionDescriptor(at); !ok {
			t.Errorf("missing descriptor for %q", at)
		}
	}
}

func TestDealDamageDecodeAndValidate(t *testing.T) {
	d, _ := lookupActionDescriptor(ActionDealDamage)
	cfg, err := d.Decode([]byte(`{"amount":140,"type":"fire"}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if issues := d.Validate(cfg, ValidationScope{}); len(issues) != 0 {
		t.Fatalf("unexpected issues: %+v", issues)
	}
	bad, _ := d.Decode([]byte(`{"amount":0,"type":"fire"}`))
	if issues := d.Validate(bad, ValidationScope{}); len(issues) == 0 {
		t.Fatalf("expected issue for zero damage")
	}
}

func TestRestoreHealthValidate(t *testing.T) {
	d, _ := lookupActionDescriptor(ActionRestoreHealth)
	good, _ := d.Decode([]byte(`{"amount":15,"school":"holy"}`))
	if issues := d.Validate(good, ValidationScope{}); len(issues) != 0 {
		t.Fatalf("unexpected: %+v", issues)
	}
	bad, _ := d.Decode([]byte(`{"amount":0}`))
	if issues := d.Validate(bad, ValidationScope{}); len(issues) == 0 {
		t.Fatalf("expected issue for zero heal")
	}
}
