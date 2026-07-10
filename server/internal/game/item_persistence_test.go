package game

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// itemOverlayCleanup removes an overlay entry after the test (process-global).
func itemOverlayCleanup(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		runtimeItemsMu.Lock()
		delete(runtimeItems, id)
		runtimeItemsMu.Unlock()
	})
}

// TestItemOverlay_LoadFromDirOverlaysDisk: an item JSON in the writable dir
// becomes visible through getItemDef/ListItemDefs, flagged Overridden.
func TestItemOverlay_LoadFromDirOverlaysDisk(t *testing.T) {
	const id = "test_overlay_item"
	itemOverlayCleanup(t, id)
	def := &ItemDef{ID: id, DisplayName: "Overlay Item", IconKey: id, Kind: ItemKindEquipment, Tier: ItemTierCommon, SlotKind: "any", Modifiers: &ItemModifiers{Armor: 3}}
	raw, err := renderItemDefJSON(def)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, present := getItemDef(id); present {
		t.Fatalf("item %q unexpectedly present before load", id)
	}
	if n := loadPersistedItemsFromDir(dir); n < 1 {
		t.Fatalf("expected >=1 item loaded, got %d", n)
	}
	got, ok := getItemDef(id)
	if !ok {
		t.Fatal("overlay item not visible after load")
	}
	if !got.Overridden {
		t.Error("overlay item must be flagged Overridden")
	}
	if got.Modifiers == nil || got.Modifiers.Armor != 3 {
		t.Errorf("payload lost in round-trip: %+v", got.Modifiers)
	}
	// ListItemDefs contains it exactly once.
	count := 0
	for _, d := range ListItemDefs() {
		if d.ID == id {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ListItemDefs contains overlay item %d times, want 1", count)
	}
}

// TestItemOverlay_OverlayWinsOverEmbed: overriding a shipped item id replaces
// it in reads; DeleteItemOverride restores the embedded version.
func TestItemOverlay_OverlayWinsOverEmbed(t *testing.T) {
	const id = "leather_armor" // shipped item
	itemOverlayCleanup(t, id)
	embedded, ok := getItemDef(id)
	if !ok {
		t.Skip("leather_armor not in catalog")
	}
	override := *embedded
	override.DisplayName = "EDITED Leather"
	override.Overridden = true
	runtimeItemsMu.Lock()
	runtimeItems[id] = &override
	runtimeItemsMu.Unlock()

	got, _ := getItemDef(id)
	if got.DisplayName != "EDITED Leather" {
		t.Fatalf("overlay must win: got %q", got.DisplayName)
	}
	runtimeItemsMu.Lock()
	delete(runtimeItems, id)
	runtimeItemsMu.Unlock()
	got, _ = getItemDef(id)
	if got.DisplayName == "EDITED Leather" {
		t.Fatal("embed must be restored after overlay removal")
	}
}

// TestRenderItemDefJSON_AuthoredFormNotWireForm guards the disk format: a def
// with a proc REFERENCE must serialize the reference (effect id), never the
// resolved wire payload (damageType/projectileID), which would freeze resolved
// values as overrides on reload.
func TestRenderItemDefJSON_AuthoredFormNotWireForm(t *testing.T) {
	def := &ItemDef{ID: "x", DisplayName: "X", IconKey: "x", Kind: ItemKindEquipment, Tier: ItemTierRare, SlotKind: "any",
		OnHitProc: &ItemOnHitProc{Chance: 0.1, Effect: "fire_bolt_ignite"}}
	raw, err := renderItemDefJSON(def)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, `"effect": "fire_bolt_ignite"`) {
		t.Errorf("disk form must keep the effect reference:\n%s", s)
	}
	if strings.Contains(s, "damageType") || strings.Contains(s, "projectileID") {
		t.Errorf("disk form must NOT contain resolved wire fields:\n%s", s)
	}
	// And the disk form round-trips through the normal parser.
	var back ItemDef
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if back.OnHitProc == nil || back.OnHitProc.Effect != "fire_bolt_ignite" || back.OnHitProc.Damage != 0 {
		t.Errorf("round-trip drifted: %+v", back.OnHitProc)
	}
}

// TestItemOverlay_SkipsMalformedAndIconsDir: bad files are skipped without
// panic; _icons/ and lists/ are not parsed as defs.
func TestItemOverlay_SkipsMalformedAndIconsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"_icons", "lists"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub, "junk.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if n := loadPersistedItemsFromDir(dir); n != 0 {
		t.Fatalf("expected 0 loaded from malformed/skipped dirs, got %d", n)
	}
}

// TestSaveItemDef_WritesTierPathAndRegistersLive: SaveItemDef writes to
// <dir>/<category>/<tier>/<id>.json and the def is immediately visible.
func TestSaveItemDef_WritesTierPathAndRegistersLive(t *testing.T) {
	const id = "test_saved_item"
	itemOverlayCleanup(t, id)
	dir := t.TempDir()
	t.Setenv("ITEM_CATALOG_DIR", dir)
	def := &ItemDef{ID: id, DisplayName: "Saved", IconKey: id, Kind: ItemKindEquipment, Tier: ItemTierUncommon, Category: "Shield", SlotKind: "any", Modifiers: &ItemModifiers{Armor: 7}}
	if err := SaveItemDef(def); err != nil {
		t.Fatalf("save: %v", err)
	}
	want := filepath.Join(dir, "shields", "uncommon", id+".json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected file at %s: %v", want, err)
	}
	if got, ok := getItemDef(id); !ok || !got.Overridden || got.Modifiers.Armor != 7 {
		t.Fatalf("live registration failed: ok=%v def=%+v", ok, got)
	}
	// Invalid defs are rejected before any write.
	bad := &ItemDef{ID: "Bad ID!", DisplayName: "x", IconKey: "x"}
	if err := SaveItemDef(bad); err == nil {
		t.Error("expected id-format validation error")
	}
}

// TestItemOverlay_VisibleToNewMatchCatalog: an overlay item registered before
// GameState construction appears in the per-match item catalog snapshot, so
// it is equippable/purchasable in NEW matches (running matches keep their
// snapshot — that semantics is deliberate and unchanged).
func TestItemOverlay_VisibleToNewMatchCatalog(t *testing.T) {
	const id = "test_match_visible_item"
	itemOverlayCleanup(t, id)
	reg := &ItemDef{ID: id, DisplayName: "Match Visible", IconKey: id, Kind: ItemKindEquipment, Tier: ItemTierCommon, SlotKind: "any", Overridden: true}
	runtimeItemsMu.Lock()
	runtimeItems[id] = reg
	runtimeItemsMu.Unlock()

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x17E4)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.itemCatalog[id]; !ok {
		t.Fatal("overlay item missing from the per-match catalog snapshot — editor items would be unusable in matches")
	}
}

// TestDeleteItemOverride_RemovesFileAndOverlay.
func TestDeleteItemOverride_RemovesFileAndOverlay(t *testing.T) {
	const id = "test_delete_item"
	itemOverlayCleanup(t, id)
	dir := t.TempDir()
	t.Setenv("ITEM_CATALOG_DIR", dir)
	def := &ItemDef{ID: id, DisplayName: "Doomed", IconKey: id, Kind: ItemKindEquipment, Tier: ItemTierCommon, SlotKind: "any"}
	if err := SaveItemDef(def); err != nil {
		t.Fatalf("save: %v", err)
	}
	existed, err := DeleteItemOverride(id)
	if err != nil || !existed {
		t.Fatalf("delete: existed=%v err=%v", existed, err)
	}
	if _, ok := getItemDef(id); ok {
		t.Fatal("editor-created item must vanish after delete")
	}
	if existed, _ := DeleteItemOverride(id); existed {
		t.Fatal("second delete must report not-existed")
	}
}

// tinyPNG renders a 4x4 PNG in memory.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestSaveItemIcon_RoundTripAndIconKeyForce.
func TestSaveItemIcon_RoundTripAndIconKeyForce(t *testing.T) {
	const id = "test_icon_item"
	itemOverlayCleanup(t, id)
	dir := t.TempDir()
	t.Setenv("ITEM_CATALOG_DIR", dir)
	def := &ItemDef{ID: id, DisplayName: "Icon", IconKey: "something_else", Kind: ItemKindEquipment, Tier: ItemTierCommon, SlotKind: "any"}
	if err := SaveItemDef(def); err != nil {
		t.Fatalf("save def: %v", err)
	}
	data := tinyPNG(t)
	if err := SaveItemIcon(id, data); err != nil {
		t.Fatalf("save icon: %v", err)
	}
	back, ok := ReadItemIcon(id)
	if !ok || !bytes.Equal(back, data) {
		t.Fatalf("icon round-trip failed: ok=%v len=%d", ok, len(back))
	}
	// IconKey forced to the item id so the URL mapping is unambiguous.
	if got, _ := getItemDef(id); got.IconKey != id {
		t.Errorf("iconKey = %q, want %q", got.IconKey, id)
	}
	// Non-PNG rejected.
	if err := SaveItemIcon(id, []byte("not a png")); err == nil {
		t.Error("expected PNG validation error")
	}
	// Oversize rejected.
	if err := SaveItemIcon(id, make([]byte, 300*1024)); err == nil {
		t.Error("expected size-cap error")
	}
	// Unknown item rejected.
	if err := SaveItemIcon("no_such_item_xyz", data); err == nil {
		t.Error("expected unknown-item error")
	}
}

// TestDeleteItemOverride_RejectsTraversalIDs: ids that fail the pattern are
// never joined into filesystem paths (Windows backslash traversal included).
func TestDeleteItemOverride_RejectsTraversalIDs(t *testing.T) {
	t.Setenv("ITEM_CATALOG_DIR", t.TempDir())
	for _, id := range []string{`..\..\evil`, "../evil", "a/b", `a\b`, "UPPER", ""} {
		if existed, err := DeleteItemOverride(id); err != nil || existed {
			t.Errorf("id %q: want (false,nil), got (%v,%v)", id, existed, err)
		}
	}
	if err := SaveItemIcon(`..\evil`, []byte("x")); err == nil {
		t.Error("SaveItemIcon must reject pattern-failing ids")
	}
}

// TestUpgradeGrant_UsesMatchSnapshotNotLiveOverlay: an item added to the live
// overlay AFTER match creation is not resolvable by in-match upgrade grants —
// running matches are isolated; new matches pick it up via the snapshot.
func TestUpgradeGrant_UsesMatchSnapshotNotLiveOverlay(t *testing.T) {
	const id = "test_post_match_item"
	itemOverlayCleanup(t, id)
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x15EA)
	runtimeItemsMu.Lock()
	runtimeItems[id] = &ItemDef{ID: id, DisplayName: "Late", IconKey: id, Kind: ItemKindEquipment, Tier: ItemTierCommon, SlotKind: "any", Overridden: true}
	runtimeItemsMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.itemCatalog[id]; ok {
		t.Fatal("snapshot must not see post-creation overlay items")
	}
}
