package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// ProfileCorruptError is returned when both the primary and backup profile
// files exist but cannot be parsed — the profile is unrecoverable without
// manual intervention.
type ProfileCorruptError struct {
	PlayerID string
	PrimaryErr error
	BackupErr  error
}

func (e *ProfileCorruptError) Error() string {
	return fmt.Sprintf("profile %q is corrupt: primary=%v, backup=%v", e.PlayerID, e.PrimaryErr, e.BackupErr)
}

// validPlayerIDRe rejects any player ID that is not a lowercase UUID.
// This prevents path traversal: only hex digits and hyphens, exactly 36 chars.
var validPlayerIDRe = regexp.MustCompile(`^[0-9a-f-]{36}$`)

// Store handles atomic file-based read/write for player profiles.
type Store interface {
	Load(playerID string) (*PlayerProfile, error)
	Save(playerID string, p *PlayerProfile) error
}

type fileStore struct {
	dir string
}

// NewFileStore returns a Store backed by files under dir.
func NewFileStore(dir string) Store {
	return &fileStore{dir: dir}
}

func (s *fileStore) validateID(playerID string) error {
	if !validPlayerIDRe.MatchString(playerID) {
		return fmt.Errorf("invalid player ID %q: must match [0-9a-f-]{36}", playerID)
	}
	return nil
}

func (s *fileStore) ensureDir() error {
	return os.MkdirAll(s.dir, 0o755)
}

func (s *fileStore) primaryPath(playerID string) string {
	return filepath.Join(s.dir, playerID+".json")
}

func (s *fileStore) tmpPath(playerID string) string {
	return filepath.Join(s.dir, playerID+".json.tmp")
}

func (s *fileStore) bakPath(playerID string) string {
	return filepath.Join(s.dir, playerID+".json.bak")
}

func (s *fileStore) Load(playerID string) (*PlayerProfile, error) {
	if err := s.validateID(playerID); err != nil {
		return nil, err
	}

	primary := s.primaryPath(playerID)
	data, err := os.ReadFile(primary)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profile %q: %w", playerID, err)
	}

	var p PlayerProfile
	if err := json.Unmarshal(data, &p); err == nil {
		migrateProfile(&p)
		return &p, nil
	}
	primaryErr := err

	// Primary parse failed — try backup.
	bak := s.bakPath(playerID)
	bakData, bakReadErr := os.ReadFile(bak)
	if bakReadErr != nil {
		return nil, &ProfileCorruptError{PlayerID: playerID, PrimaryErr: primaryErr, BackupErr: bakReadErr}
	}
	var bp PlayerProfile
	if err2 := json.Unmarshal(bakData, &bp); err2 != nil {
		return nil, &ProfileCorruptError{PlayerID: playerID, PrimaryErr: primaryErr, BackupErr: err2}
	}
	migrateProfile(&bp)
	return &bp, nil
}

// migrateProfile applies forward migrations to bring a profile up to
// CurrentVersion. Safe to call on a profile at any version including
// CurrentVersion (idempotent). The Version field is updated to CurrentVersion
// so that the next Save writes the new schema.
func migrateProfile(p *PlayerProfile) {
	// v1 -> v2: initialize OwnedUpgradeRanks.
	if p.OwnedUpgradeRanks == nil {
		p.OwnedUpgradeRanks = map[string]int{}
	}
	// v2 -> v3: initialize ActiveUpgradeIDs. Any upgrade with rank > 0 is
	// activated by default so existing owners keep their upgrades active.
	if p.ActiveUpgradeIDs == nil {
		active := make([]string, 0, len(p.OwnedUpgradeRanks))
		for id, rank := range p.OwnedUpgradeRanks {
			if rank > 0 {
				active = append(active, id)
			}
		}
		sort.Strings(active)
		p.ActiveUpgradeIDs = active
	}
	// v3 -> v4: initialize AcquiredAdvancements. A nil slice is treated as
	// empty; we normalise to a non-nil empty slice so JSON serialisation
	// produces [] rather than null and downstream code can range over it safely.
	if p.AcquiredAdvancements == nil {
		p.AcquiredAdvancements = []AcquiredAdvancement{}
	}
	// Stamp current version so the next Save persists the new schema.
	p.Version = CurrentVersion
}

func (s *fileStore) Save(playerID string, p *PlayerProfile) error {
	if err := s.validateID(playerID); err != nil {
		return err
	}
	if err := s.ensureDir(); err != nil {
		return fmt.Errorf("profile dir: %w", err)
	}

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal profile %q: %w", playerID, err)
	}

	primary := s.primaryPath(playerID)
	tmp := s.tmpPath(playerID)
	bak := s.bakPath(playerID)

	// Back up the current file before overwriting it.
	if _, statErr := os.Stat(primary); statErr == nil {
		if cpErr := copyFile(primary, bak); cpErr != nil {
			return fmt.Errorf("backup profile %q: %w", playerID, cpErr)
		}
	}

	// Write to tmp, sync, then rename atomically.
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open tmp profile %q: %w", playerID, err)
	}
	if _, werr := f.Write(data); werr != nil {
		_ = f.Close()
		return fmt.Errorf("write tmp profile %q: %w", playerID, werr)
	}
	if serr := f.Sync(); serr != nil {
		_ = f.Close()
		return fmt.Errorf("sync tmp profile %q: %w", playerID, serr)
	}
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("close tmp profile %q: %w", playerID, cerr)
	}
	if rerr := os.Rename(tmp, primary); rerr != nil {
		return fmt.Errorf("rename tmp profile %q: %w", playerID, rerr)
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
