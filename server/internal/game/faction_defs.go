package game

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"sort"
	"strings"
)

// factionMetaFileName is the per-faction metadata record, living beside the
// unit directories at catalog/units/<faction>/faction.json. It is NOT a unit —
// every catalog walk that expects unit directories must skip it by name.
const factionMetaFileName = "faction.json"

// FactionDef is a faction's presentation record.
//
// The DIRECTORY is the source of truth for a faction's existence; faction.json
// only adds metadata. A faction directory without one is still a perfectly
// valid faction (its record is synthesized), which is why the factions that
// predate this file need no new JSON. The record exists so that (a) a faction
// can be created in the editor before it owns any units, and (b) the editor can
// show "Witherborne" instead of "witherborne".
type FactionDef struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	// Order sorts the editor's faction filter. Ties fall back to ID.
	//
	// Zero / omitted means "unordered": those factions sort AFTER every
	// faction that declares an explicit (non-zero) Order, alphabetically
	// among themselves. This is the opposite of naive ascending-int sort —
	// see factionSortOrder — because JSON `omitempty` cannot distinguish
	// "unset" from "0", so an authored order:1 must not sort behind the
	// record-less factions sitting at the zero value.
	Order int `json:"order,omitempty"`
}

var embeddedFactions = loadEmbeddedFactions()

func loadEmbeddedFactions() map[string]FactionDef {
	entries, err := fs.ReadDir(unitDefsFS, "catalog/units")
	if err != nil {
		panic("catalog/units: " + err.Error())
	}
	result := make(map[string]FactionDef, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		def := defaultFactionDef(id)
		path := "catalog/units/" + id + "/" + factionMetaFileName
		raw, rerr := unitDefsFS.ReadFile(path)
		switch {
		case errors.Is(rerr, fs.ErrNotExist):
			// no record — keep the synthesized default
		case rerr != nil:
			panic(path + ": " + rerr.Error())
		default:
			var parsed FactionDef
			if uerr := json.Unmarshal(raw, &parsed); uerr != nil {
				panic(path + ": " + uerr.Error())
			}
			if parsed.ID != "" && parsed.ID != id {
				panic(path + `: id "` + parsed.ID + `" does not match directory "` + id + `"`)
			}
			def = normalizeFactionDef(id, parsed)
		}
		result[id] = def
	}
	return result
}

// defaultFactionDef is the record a faction directory gets when it has no
// faction.json of its own.
func defaultFactionDef(id string) FactionDef {
	return FactionDef{ID: id, DisplayName: titleizeFactionID(id)}
}

// normalizeFactionDef forces the record's id to match its directory and fills a
// blank display name, so a hand-written or editor-posted record is always
// coherent with where it lives.
func normalizeFactionDef(id string, def FactionDef) FactionDef {
	def.ID = id
	if strings.TrimSpace(def.DisplayName) == "" {
		def.DisplayName = titleizeFactionID(id)
	}
	return def
}

// titleizeFactionID turns "witherborne" into "Witherborne" and "night_elf" into
// "Night Elf" — a readable fallback label for a faction with no record.
func titleizeFactionID(id string) string {
	words := strings.Split(id, "_")
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// factionSortOrder maps a record's Order to its sort key. Zero/omitted means
// "unordered": those factions sort after every faction that declared an order,
// alphabetically among themselves. Without this, an authored order:1 would sort
// BEHIND every record-less faction sitting at the zero value.
func factionSortOrder(def FactionDef) int {
	if def.Order == 0 {
		return math.MaxInt32
	}
	return def.Order
}

// ListFactions returns every known faction, sorted by Order then ID. See
// factionSortOrder for how a zero/omitted Order is treated.
//
// A faction is "known" if it has an embedded directory OR any unit claims it.
// The second clause matters: an editor-created unit can be saved into a brand
// new faction, and the filter must never hide a unit that exists.
func ListFactions() []FactionDef {
	merged := make(map[string]FactionDef, len(embeddedFactions))
	for id, def := range embeddedFactions {
		merged[id] = def
	}
	runtimeFactionsMu.RLock()
	for id, def := range runtimeFactions {
		merged[id] = def
	}
	runtimeFactionsMu.RUnlock()
	for _, unit := range ListUnitDefs() {
		if unit.Faction == "" {
			continue
		}
		if _, ok := merged[unit.Faction]; !ok {
			merged[unit.Faction] = defaultFactionDef(unit.Faction)
		}
	}
	out := make([]FactionDef, 0, len(merged))
	for _, def := range merged {
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool {
		oi, oj := factionSortOrder(out[i]), factionSortOrder(out[j])
		if oi != oj {
			return oi < oj
		}
		return out[i].ID < out[j].ID
	})
	return out
}
