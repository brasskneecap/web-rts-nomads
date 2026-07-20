package game

import "testing"

func TestUnitTypeForPath(t *testing.T) {
	cases := map[string]string{
		"siphoner": "acolyte", "cleric": "acolyte", "arch_mage": "adept",
		"marksman": "archer", "trapper": "archer",
		"berserker": "soldier", "vanguard": "soldier",
		"does_not_exist": "",
	}
	for path, want := range cases {
		if got := unitTypeForPath(path); got != want {
			t.Errorf("unitTypeForPath(%q) = %q, want %q", path, got, want)
		}
	}
}
