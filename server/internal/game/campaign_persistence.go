package game

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ─── Writable campaign header overlay ────────────────────────────────────────
//
// Mirrors the map/item persistence systems (maps.go, item_persistence.go):
// editor saves write a header JSON file into a writable dir and register it
// into an in-memory overlay that WINS over the embedded campaign headers in
// every reader (buildCampaignDefs, validateMapCampaignBlockBasics). Loaded once
// at startup by LoadPersistedCampaignsIntoOverlay; per-file failures are logged
// skips so the server always starts.
//
// Only the HEADER is authored here (id / displayName / description / sortOrder /
// locked). A campaign's LEVELS stay derived from campaign-tagged maps — see the
// CampaignDef doc in campaign_defs.go — so this file never touches levels.

var (
	runtimeCampaignHeadersMu sync.RWMutex
	runtimeCampaignHeaders   = map[string]CampaignDef{}
)

// campaignIDPattern is the id discipline for author-created campaigns. Embedded
// headers predate it and are exempt (validated by their own loader).
var campaignIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// currentCampaignHeaders returns the embedded header baseline with runtime
// editor saves overlaid on top (overlay wins). Callers treat the result as
// read-only. Cheap; the catalog is tiny.
func currentCampaignHeaders() map[string]CampaignDef {
	merged := make(map[string]CampaignDef, len(campaignHeadersByID))
	for id, h := range campaignHeadersByID {
		merged[id] = h
	}
	runtimeCampaignHeadersMu.RLock()
	for id, h := range runtimeCampaignHeaders {
		merged[id] = h
	}
	runtimeCampaignHeadersMu.RUnlock()
	return merged
}

func resolveCampaignsDir() (string, error) {
	if dir := os.Getenv("CAMPAIGN_CATALOG_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "internal", "game", "catalog", "campaigns")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("campaigns directory not found at %s; set CAMPAIGN_CATALOG_DIR env var to override", dir)
}

// CampaignHeaderInput is the authored header shape accepted by the editor's
// POST /api/catalog/campaigns. Levels are intentionally absent — they derive
// from campaign-tagged maps.
type CampaignHeaderInput struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	SortOrder   int    `json:"sortOrder"`
	Locked      bool   `json:"locked"`
}

// IsCampaignSaveValidationError reports whether err is an author-fixable
// campaign save/delete failure (→ HTTP 400) rather than infrastructure (→ 500).
// Campaign save errors reuse the shared campaignSaveError type.
func IsCampaignSaveValidationError(err error) bool { return IsMapSaveValidationError(err) }

// CampaignIsBuiltIn reports whether id is an embedded (compile-time) campaign.
// Built-ins can be edited (an overlay file shadows them) but not deleted.
func CampaignIsBuiltIn(id string) bool {
	_, ok := campaignHeadersByID[id]
	return ok
}

// SaveCampaignHeader validates and persists a campaign header, then registers
// it into the runtime overlay so it is visible without a restart. Returns a
// campaignSaveError (→ HTTP 400) for bad input.
func SaveCampaignHeader(in CampaignHeaderInput) error {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return errCampaignSave("campaign id is required")
	}
	if !campaignIDPattern.MatchString(id) {
		return errCampaignSave(fmt.Sprintf("campaign id %q must match %s", id, campaignIDPattern))
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return errCampaignSave("campaign displayName is required")
	}
	dir, err := resolveCampaignsDir()
	if err != nil {
		return err
	}
	def := CampaignDef{
		ID:          id,
		DisplayName: strings.TrimSpace(in.DisplayName),
		Description: in.Description,
		SortOrder:   in.SortOrder,
		Locked:      in.Locked,
	}
	if werr := writeCampaignHeaderToDisk(dir, def); werr != nil {
		return werr
	}
	runtimeCampaignHeadersMu.Lock()
	runtimeCampaignHeaders[id] = def
	runtimeCampaignHeadersMu.Unlock()
	return nil
}

// writeCampaignHeaderToDisk serializes a header (never its derived levels) to
// <dir>/<id>.json. Reuses the maps filename sanitizer for a safe path.
func writeCampaignHeaderToDisk(dir string, def CampaignDef) error {
	safeID := sanitizeMapFilename(def.ID)
	if safeID == "" {
		return fmt.Errorf("campaign id %q is not a valid filename", def.ID)
	}
	payload := CampaignHeaderInput{
		ID:          def.ID,
		DisplayName: def.DisplayName,
		Description: def.Description,
		SortOrder:   def.SortOrder,
		Locked:      def.Locked,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(filepath.Join(dir, safeID+".json"), raw, 0644)
}

// mapsReferencingCampaign returns, sorted, the ids of maps whose campaign block
// targets campaignID.
func mapsReferencingCampaign(campaignID string) []string {
	var ids []string
	for _, entry := range currentMapCatalogSnapshot() {
		if entry.Map.Campaign != nil && entry.Map.Campaign.CampaignID == campaignID {
			ids = append(ids, entry.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// DeleteCampaignDef removes an author-created campaign header. Guards:
//   - refuses if any map still references the campaign (deleting the header
//     would orphan a campaignId, which panics discovery) — the caller must
//     remove those levels first;
//   - refuses to delete a built-in (embedded) campaign.
//
// Returns whether an overlay entry existed and a campaignSaveError (→ 400) for
// a guard failure.
func DeleteCampaignDef(id string) (existed bool, err error) {
	if refs := mapsReferencingCampaign(id); len(refs) > 0 {
		return false, errCampaignSave(
			"campaign is still used by maps: " + strings.Join(refs, ", ") +
				" — remove those levels from the campaign first")
	}
	if CampaignIsBuiltIn(id) {
		return false, errCampaignSave("cannot delete a built-in campaign")
	}
	dir, derr := resolveCampaignsDir()
	if derr != nil {
		return false, derr
	}
	safeID := sanitizeMapFilename(id)
	if safeID == "" {
		return false, errCampaignSave("invalid campaign id")
	}
	runtimeCampaignHeadersMu.Lock()
	_, existed = runtimeCampaignHeaders[id]
	delete(runtimeCampaignHeaders, id)
	runtimeCampaignHeadersMu.Unlock()

	if rmErr := os.Remove(filepath.Join(dir, safeID+".json")); rmErr != nil && !os.IsNotExist(rmErr) {
		return existed, rmErr
	}
	return existed, nil
}

// LoadPersistedCampaignsIntoOverlay loads editor-saved campaign headers at
// startup so they survive a restart. Best-effort, never fatal (mirrors
// LoadPersistedMapsIntoOverlay). In dev the writable dir IS the embed source
// dir, so re-reading the built-in headers is a harmless no-op overlay.
func LoadPersistedCampaignsIntoOverlay() {
	dir, err := resolveCampaignsDir()
	if err != nil {
		slog.Info("persisted campaigns: no writable dir; using embedded headers only", "err", err)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Info("persisted campaigns: read dir failed; using embedded headers only", "err", err)
		return
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if rerr != nil {
			slog.Warn("persisted campaigns: skip unreadable file", "file", entry.Name(), "err", rerr)
			continue
		}
		var in CampaignHeaderInput
		if jerr := json.Unmarshal(data, &in); jerr != nil {
			slog.Warn("persisted campaigns: skip malformed file", "file", entry.Name(), "err", jerr)
			continue
		}
		if in.ID == "" || in.DisplayName == "" {
			slog.Warn("persisted campaigns: skip file missing id/displayName", "file", entry.Name())
			continue
		}
		runtimeCampaignHeadersMu.Lock()
		runtimeCampaignHeaders[in.ID] = CampaignDef{
			ID:          in.ID,
			DisplayName: in.DisplayName,
			Description: in.Description,
			SortOrder:   in.SortOrder,
			Locked:      in.Locked,
		}
		runtimeCampaignHeadersMu.Unlock()
		count++
	}
	if count > 0 {
		slog.Info("persisted campaigns: overlaid on embedded headers", "count", count, "dir", dir)
	}
}
