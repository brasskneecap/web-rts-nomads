package game

import "fmt"

// EditorProjectileSaveRequest is the body of POST /projectiles.
type EditorProjectileSaveRequest struct {
	Projectile ProjectileDef `json:"projectile"`
}

// SaveEditorProjectile validates then persists an authored projectile def.
// Validation failures are wrapped as editorValidationError so the handler
// returns 400.
func SaveEditorProjectile(req EditorProjectileSaveRequest) error {
	projectile := req.Projectile
	if !projectileIDPattern.MatchString(projectile.ID) {
		return editorValidationError{fmt.Errorf("projectile id %q must match %s", projectile.ID, projectileIDPattern)}
	}
	if err := validateProjectileDef(&projectile); err != nil {
		return editorValidationError{err}
	}
	return SaveProjectileDef(&projectile)
}

// DeleteEditorProjectile removes an override; embed-backed ids reset to default.
func DeleteEditorProjectile(id string) (existed bool, err error) {
	return DeleteProjectileOverride(id)
}
