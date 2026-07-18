package game

import (
	"encoding/json"
	"strings"
	"testing"
)

// greaterHealV2JSON is the canonical design-doc v2 fixture for Greater Heal.
// Pasted verbatim from the composable-ability-system Phase 2 design doc; this
// test locks that JSON shape to the Go AbilityDef / AbilityProgram types.
const greaterHealV2JSON = `{
  "id": "greater_heal",
  "displayName": "Greater Heal",
  "type": "spell",
  "category": "heal",
  "damageType": "holy",
  "icon": "TODO/abilities/greater_heal.png",
  "manaCost": 10,
  "cooldown": 3,
  "castTime": 1.0,
  "casterAnimation": "Casting",
  "supportsAutoCast": true,
  "autoCastTargetSelector": "lowest_hp_percentage_ally_in_range",
  "defaultAutoCast": true,
  "schemaVersion": 2,
  "program": {
    "entry": { "type": "unit", "relations": ["self", "ally"], "range": "match_attack_range" },
    "triggers": [
      {
        "id": "t_cast", "type": "on_cast_complete",
        "actions": [
          {
            "id": "a_select", "type": "select_targets",
            "target": {
              "source": "all_in_scene", "origin": "caster", "relations": ["self", "ally"],
              "radius": -1, "ordering": "lowest_health_percentage",
              "maxCount": 3, "includeInitialTarget": true
            },
            "outputs": { "targets": "healTargets" }
          },
          {
            "id": "a_heal", "type": "restore_health",
            "input": { "targets": { "key": "healTargets" } },
            "config": { "amount": 15, "school": "holy" }
          },
          {
            "id": "a_vfx", "type": "play_presentation",
            "input": { "attach": { "key": "healTargets" } },
            "config": { "asset": "healing_glow", "oncePerTarget": true }
          }
        ]
      }
    ]
  }
}`

// meteorV2JSON is the canonical design-doc v2 fixture for Meteor. Pasted
// verbatim; exercises nested presentations, animation-marker triggers, a
// create_zone action with its own nested zone-tick triggers, and layer swaps.
const meteorV2JSON = `{
  "id": "meteor",
  "displayName": "Meteor",
  "type": "spell",
  "category": "offensive",
  "damageType": "fire",
  "tags": ["aoe", "damage", "dot"],
  "icon": "TODO/abilities/meteor.png",
  "manaCost": 40,
  "cooldown": 12,
  "castTime": 0.8,
  "supportsAutoCast": true,
  "autoCastTargetSelector": "closest_enemy_in_range",
  "defaultAutoCast": true,
  "schemaVersion": 2,
  "program": {
    "entry": { "type": "ground_point", "relations": ["enemy"], "range": 400 },
    "triggers": [
      {
        "id": "t_cast", "type": "on_cast_complete",
        "actions": [
          {
            "id": "a_meteor", "type": "play_presentation",
            "config": {
              "asset": "meteor", "position": { "key": "castPoint" },
              "scale": 3, "renderLayer": "in_front_of_units",
              "presentationId": "p_meteor"
            }
          }
        ]
      }
    ],
    "presentations": [
      {
        "id": "p_meteor", "asset": "meteor",
        "position": { "key": "castPoint" }, "scale": 3, "renderLayer": "in_front_of_units",
        "triggers": [
          {
            "id": "t_cross", "type": "on_animation_marker", "timing": { "marker": "cross_unit_plane" },
            "actions": [
              { "id": "a_layer", "type": "change_render_layer", "config": { "layer": "behind_units" } }
            ]
          },
          {
            "id": "t_impact", "type": "on_animation_marker", "timing": { "marker": "impact" },
            "actions": [
              {
                "id": "a_sel", "type": "select_targets",
                "target": { "source": "all_in_scene", "origin": "impact_position", "radius": 230, "relations": ["enemy"] },
                "outputs": { "targets": "hitEnemies" }
              },
              {
                "id": "a_dmg", "type": "deal_damage",
                "input": { "targets": { "key": "hitEnemies" } },
                "config": { "amount": 140, "type": "fire" }
              },
              {
                "id": "a_zone", "type": "create_zone",
                "config": {
                  "name": "Burning Crater", "position": { "key": "impactPosition" },
                  "anchor": "ground", "radius": 120, "duration": 4, "tickInterval": 0.5,
                  "owner": { "key": "caster" }, "presentation": "burning_crater",
                  "triggers": [
                    {
                      "id": "t_burn", "type": "on_zone_tick", "timing": { "tickInterval": 0.5 },
                      "actions": [
                        {
                          "id": "a_bsel", "type": "select_targets",
                          "target": { "source": "all_in_scene", "origin": "zone_center", "radius": 120, "relations": ["enemy"] },
                          "outputs": { "targets": "burnHits" }
                        },
                        {
                          "id": "a_bdmg", "type": "deal_damage",
                          "input": { "targets": { "key": "burnHits" } },
                          "config": { "amount": 12, "type": "fire" }
                        }
                      ]
                    }
                  ]
                }
              }
            ]
          }
        ]
      }
    ]
  }
}`

func TestGreaterHealV2Fixture(t *testing.T) {
	var def AbilityDef
	if err := json.Unmarshal([]byte(greaterHealV2JSON), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if def.Program == nil {
		t.Fatal("program nil")
	}
	if def.SchemaVersion != 2 {
		t.Fatalf("schemaVersion=%d", def.SchemaVersion)
	}
	if got := def.Program.Triggers[0].Type; got != TriggerOnCastComplete {
		t.Fatalf("trigger type=%q", got)
	}
	if err := validateAbilityDef(&def); err != nil {
		t.Fatalf("greater_heal v2 invalid: %v", err)
	}
	out, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "healing_glow") {
		t.Errorf("round-trip lost healing_glow: %s", out)
	}
}

func TestMeteorV2Fixture(t *testing.T) {
	var def AbilityDef
	if err := json.Unmarshal([]byte(meteorV2JSON), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if def.Program == nil || len(def.Program.Presentations) == 0 {
		t.Fatal("meteor presentation missing")
	}
	if err := validateAbilityDef(&def); err != nil {
		t.Fatalf("meteor v2 invalid: %v", err)
	}
	out, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	for _, want := range []string{"burning_crater", "Burning Crater", "cross_unit_plane"} {
		if !strings.Contains(s, want) {
			t.Errorf("round-trip lost %q", want)
		}
	}
}
