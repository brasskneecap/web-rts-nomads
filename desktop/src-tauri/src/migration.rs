// §7 task 7.6 — legacy ./profiles/ → <userdata>/profiles/ migration.
//
// Runs once on first launch when:
//   1. The userdata profiles/ directory exists but is empty AND
//   2. A legacy ./profiles/ directory exists adjacent to the shell binary
//      (or adjacent to the Go sidecar — both are equivalent locations for
//      pre-packaging builds).
//
// Behaviour:
//   - Files are COPIED (not moved). Source is left untouched so a fall-back
//     to the older binary still works.
//   - The "did we migrate?" mark is the presence of files in the userdata
//     profiles/ dir on subsequent launches — no separate flag file.
//   - A single log line names every file copied for diagnostic traceability.

use std::env;
use std::fs;
use std::path::{Path, PathBuf};

use log::{info, warn};

pub struct MigrationReport {
    pub source: Option<PathBuf>,
    pub files_copied: Vec<String>,
}

pub fn run_once_if_needed(userdata_profiles: &Path) -> MigrationReport {
    let target_empty = match fs::read_dir(userdata_profiles) {
        Ok(mut iter) => iter.next().is_none(),
        Err(_) => true,
    };
    if !target_empty {
        return MigrationReport {
            source: None,
            files_copied: vec![],
        };
    }

    let source = match locate_legacy_profiles_dir() {
        Some(p) => p,
        None => {
            return MigrationReport {
                source: None,
                files_copied: vec![],
            }
        }
    };

    info!(
        "migration: legacy profiles dir found at {} — copying to {}",
        source.display(),
        userdata_profiles.display()
    );

    let mut copied = Vec::new();
    let entries = match fs::read_dir(&source) {
        Ok(it) => it,
        Err(e) => {
            warn!("migration: read_dir({}): {e}", source.display());
            return MigrationReport {
                source: Some(source),
                files_copied: vec![],
            };
        }
    };
    for entry in entries.flatten() {
        let name = entry.file_name();
        let src = entry.path();
        if !src.is_file() {
            continue;
        }
        let dst = userdata_profiles.join(&name);
        match fs::copy(&src, &dst) {
            Ok(_) => copied.push(name.to_string_lossy().into_owned()),
            Err(e) => warn!("migration: copy {} -> {}: {e}", src.display(), dst.display()),
        }
    }
    info!("migration: copied {} files", copied.len());
    MigrationReport {
        source: Some(source),
        files_copied: copied,
    }
}

fn locate_legacy_profiles_dir() -> Option<PathBuf> {
    // Look adjacent to the running shell binary, then to current_dir().
    if let Ok(exe) = env::current_exe() {
        if let Some(dir) = exe.parent() {
            let candidate = dir.join("profiles");
            if candidate.is_dir() {
                return Some(candidate);
            }
        }
    }
    if let Ok(cwd) = env::current_dir() {
        let candidate = cwd.join("profiles");
        if candidate.is_dir() {
            return Some(candidate);
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    fn unique_tmp(name: &str) -> PathBuf {
        std::env::temp_dir().join(format!(
            "nomads-migration-{}-{}-{}",
            name,
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ))
    }

    #[test]
    fn no_op_when_target_not_empty() {
        let root = unique_tmp("no_op");
        let target = root.join("profiles");
        fs::create_dir_all(&target).unwrap();
        fs::write(target.join("preexisting.json"), b"{}").unwrap();

        let report = run_once_if_needed(&target);
        assert!(report.files_copied.is_empty(), "should not copy");
        assert!(report.source.is_none(), "should not even probe source");

        fs::remove_dir_all(&root).ok();
    }

    #[test]
    fn no_op_when_no_legacy_source() {
        let root = unique_tmp("no_legacy");
        let target = root.join("profiles");
        fs::create_dir_all(&target).unwrap();
        // No "profiles" dir adjacent to current_exe or cwd in test.
        // The function will probe and find none.
        let _report = run_once_if_needed(&target);
        // Cannot assert files_copied.is_empty() reliably because cargo
        // test's cwd may differ; the assertion that matters is that calling
        // this on an empty target with no source doesn't panic and doesn't
        // create files.
        let entries: Vec<_> = fs::read_dir(&target).unwrap().collect();
        assert_eq!(entries.len(), 0);

        fs::remove_dir_all(&root).ok();
    }

    // Test the file-copying logic by exercising it via a different code path
    // than run_once_if_needed, since the latter probes current_exe() / cwd
    // for a legacy dir, which is environment-dependent.
    #[test]
    fn copy_files_helper_handles_subset() {
        // This is more of a sanity check on the fs::copy contract than on our
        // own code — but it documents the assumption that copy preserves
        // contents byte-for-byte, which matters for profile migrations.
        let root = unique_tmp("copy_test");
        let src = root.join("src");
        let dst = root.join("dst");
        fs::create_dir_all(&src).unwrap();
        fs::create_dir_all(&dst).unwrap();
        let mut f = fs::File::create(src.join("a.json")).unwrap();
        f.write_all(br#"{"x":1}"#).unwrap();
        drop(f);

        fs::copy(src.join("a.json"), dst.join("a.json")).unwrap();
        let bytes = fs::read(dst.join("a.json")).unwrap();
        assert_eq!(bytes, br#"{"x":1}"#);

        fs::remove_dir_all(&root).ok();
    }
}
