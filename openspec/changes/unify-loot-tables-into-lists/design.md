## Context

`loot_tables.json` holds three things under two keys:

| Thing | Today | Really is |
|---|---|---|
| `item_subtable` (×7) | `{kind, entries:[{item,min,max}]}` | **a weighted list** |
| `resource_bundle` (×2) | `{kind, resources:{gold,wood}}` | a gold/wood grant |
| `tables` (×3) | `[{entry, min, max}]` | **a weighted roll over the above** |

The subtables are lists with weights and nothing more. They were left out of the list unification only because they were locked in the same file as the other two, neither of which is a list.

Two facts shape the design:

- **Subtables roll their own die.** `basic_weapons` covers 1–15, `rare_weapons` 1–20, the merchant ones 1–100. The roll is `Intn(maxOfEntries)+1`, so today the die size is *implied* by the highest entry — you cannot express a trailing gap.
- **Every shipped subtable is already gap-free**, and the only gap in the catalog is `raider_loot`'s 51–60. So adopting a strict "must cover 1..maxRoll" rule costs one explicit `nothing` row and nothing else.

## Goals / Non-Goals

**Goals:**
- One grouping primitive (`ListDef`) that can be uniform or weighted, and one composition primitive (`TableDef`) that weights lists together.
- Loot becomes authorable end-to-end, like items and lists.
- No silent holes: a roll that lands nowhere is impossible unless you said so.

**Non-Goals:**
- Nested tables (a table row rolling another table). Rows point at lists, resources, or nothing. Flat.
- Per-table item overrides. If two tables want different odds for the same items, they reference two lists.
- Changing the drop *sources*. Camps remain the only thing that drops loot.

## Decisions

### D1 — `ListDef` gets a weighted form; exactly one form per list

```go
type ListDef struct {
    ID   string
    Name string
    // UNIFORM form: every member equally likely.
    Items []string `json:"items,omitempty"`
    // WEIGHTED form: each member owns a slice of a 1..MaxRoll die.
    MaxRoll int         `json:"maxRoll,omitempty"`
    Entries []ListEntry `json:"entries,omitempty"`
}
type ListEntry struct{ Item string; Min, Max int }
```

`IsWeighted()` is `len(Entries) > 0`. Setting both forms is a load error — a list has one notion of "how likely is this member", not two.

`ItemIDs()` returns the members regardless of form. Every consumer that only cares about *membership* (an Artificer's craftable scope, a Recipe Shop's pool, a marketplace's verbatim shelf) goes through it and never has to know which form it got.

*Alternative considered:* a single form where every list is weighted and a uniform list just has equal ranges. Rejected — it makes the common case (a marketplace's 12 items) verbose and forces an author to do arithmetic to add one item.

### D2 — Weights apply wherever a list is read

A weighted list sampled for shop stock is sampled *by weight*. This is a deliberate behavior change (neutral shops sample uniformly today) and it is the point of putting weights on the list rather than on the table's reference to it: "how likely is this item" is a property of the pool, so a rare sword is rare on a shelf for the same reason it is rare in a chest.

Uniform lists keep sampling uniformly, so every currently-shipped shop (`marketplace`, `wandering_merchant` — both uniform) behaves exactly as before.

### D3 — `TableDef`: a die, and rows that own ranges

```go
type TableDef struct {
    ID      string
    Name    string
    MaxRoll int
    Rows    []TableRow
}
type TableRow struct {
    Min, Max  int
    // EXACTLY ONE of:
    List      string         `json:"list,omitempty"`      // roll this list
    Resources map[string]int `json:"resources,omitempty"` // grant gold/wood
    Nothing   bool           `json:"nothing,omitempty"`   // drop nothing
}
```

Resource bundles die here. They were a named entity only so two tables could share `{gold: 50, wood: 15}`, which is not a saving worth a third catalog. A row grants resources inline.

`Nothing` is what makes gaps unnecessary: a no-drop outcome is a row you can see, name a range for, and read a percentage off — not the absence of a row.

### D4 — Coverage is total: no gaps, no overlaps

Rows (and weighted list entries) MUST tile `1..MaxRoll` exactly. Overlaps were already an error. Gaps become one.

This is the user's call and it is the right one: today's only gap (`raider_loot` 51–60) is a deliberate 10% no-drop that reads, in the JSON, as a typo. After this change it reads as `{"min":51,"max":60,"nothing":true}`. Every roll lands somewhere, and "somewhere" can be "nothing".

Weighted **lists** have no `nothing` row: a list is a pool of items, and the decision *whether* to drop belongs to the table. A weighted list, once rolled, always yields an item.

### D5 — Validation reports coverage, not just errors

The editor shows the die as a coverage strip — which ranges land where, each as a percentage — so an author sees the distribution they actually authored rather than inferring it from arithmetic. Errors (gap, overlap, out-of-range, unknown list/item) block the save; the coverage readout is informational.

### D6 — The catalog splits by entity, not by file

- `catalog/lists/<id>.json` — the 3 existing lists plus 7 migrated subtables
- `catalog/tables/<id>.json` — the 3 migrated tables

One file per entity, like items and lists, because that is what per-entity editor CRUD needs. `loot_tables.json` is deleted.

## Risks / Trade-offs

- **Weighted shop sampling is a live balance change.** A neutral shop bound to a weighted list will now favour common items. → Only lists that are *authored* weighted are affected, and no shipped shop uses one (`marketplace` and `wandering_merchant` are both uniform). The change is opt-in per list, but it IS a change, and a future author binding `merchant_weapons` to a shop will get weighted odds where they might have expected uniform ones.

- **The "no gaps" rule can silently rebalance a table if migrated carelessly.** `raider_loot`'s 51–60 gap must become a `nothing` row, not be absorbed into a neighbouring range — absorbing it would turn a 10% no-drop into 10% more loot. → The migration is asserted by a test that rolls the migrated table across all 100 faces and compares the outcome distribution to the pre-migration table, face by face.

- **`ShopLootTableID` and `NeutralGroup.LootTable` now name a `TableDef`.** The ids are unchanged (`merchant_basic`, `raider_loot`, `wildborne_loot`), so no map or neutral-group JSON changes — but the type they resolve to does. → The loader fails loudly on an unknown table id, and every shipped reference is covered by a test.

- **Deleting `SetMerchantItemAvailability` removes the only writer of loot data.** → It was already dead (test-only callers, no route), and its replacement is strictly better: edit the weighted list in the Tables/Lists tab and see the odds.
