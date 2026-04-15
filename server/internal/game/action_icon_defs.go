package game

import (
	_ "embed"
	"encoding/json"
)

//go:embed catalog/action-icons.json
var actionIconDefsJSON []byte

// ActionIconDef holds the SVG path for a named action button icon.
// These are client-only and have no server-side game logic.
type ActionIconDef struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

var actionIconDefs []ActionIconDef

func init() {
	var catalog struct {
		Icons []ActionIconDef `json:"icons"`
	}
	if err := json.Unmarshal(actionIconDefsJSON, &catalog); err != nil {
		panic("action-icons.json: " + err.Error())
	}
	actionIconDefs = catalog.Icons
}

func ListActionIconDefs() []ActionIconDef {
	return actionIconDefs
}
