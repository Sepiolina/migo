use serde::Serialize;
use sha2::{Digest, Sha256};
use std::{
    fs,
    io::{Read, Write},
    path::{Component, Path, PathBuf},
};

const LIMIT: u64 = 1024 * 1024;

#[derive(Serialize)]
pub struct Workflow {
    pub path: String,
    pub source: String,
    pub revision: String,
}
#[derive(Serialize)]
pub struct WorkflowList {
    pub paths: Vec<String>,
}

fn valid_path(path: &str) -> Result<&Path, String> {
    let path = Path::new(path);
    if path
        .components()
        .any(|c| !matches!(c, Component::Normal(_)))
        || path.as_os_str().is_empty()
    {
        return Err("Use a workspace-relative workflow path without traversal".into());
    }
    if !matches!(
        path.extension().and_then(|s| s.to_str()),
        Some("yaml" | "yml")
    ) {
        return Err("Workflow path must end in .yaml or .yml".into());
    }
    Ok(path)
}

// Reject symlinks at every level, including the file itself. The root comes from
// the native picker and is canonicalized there.
fn resolve(root: &Path, relative: &Path, allow_missing_file: bool) -> Result<PathBuf, String> {
    let mut current = root.to_path_buf();
    let count = relative.components().count();
    for (index, component) in relative.components().enumerate() {
        current.push(component.as_os_str());
        match fs::symlink_metadata(&current) {
            Ok(meta) if meta.file_type().is_symlink() => {
                return Err("Workflow paths cannot contain symlinks".into())
            }
            Ok(meta) if index + 1 < count && !meta.is_dir() => {
                return Err("Workflow parent must be a directory".into())
            }
            Ok(meta) if index + 1 == count && !meta.is_file() => {
                return Err("Workflow must be a regular file".into())
            }
            Ok(_) => (),
            Err(e)
                if e.kind() == std::io::ErrorKind::NotFound
                    && allow_missing_file
                    && index + 1 == count =>
            {
                ()
            }
            Err(_) => return Err("Cannot access workflow path".into()),
        }
    }
    let parent = current
        .parent()
        .ok_or("Invalid workflow path")?
        .canonicalize()
        .map_err(|_| "Cannot access workflow directory")?;
    if !parent.starts_with(root) {
        return Err("Workflow must be inside the workspace".into());
    }
    Ok(current)
}

fn read_at(path: &Path, relative: &str) -> Result<Workflow, String> {
    let file = fs::File::open(path).map_err(|_| "Cannot read workflow")?;
    if !file
        .metadata()
        .map_err(|_| "Cannot inspect workflow")?
        .is_file()
    {
        return Err("Workflow must be a regular file".into());
    }
    let mut bytes = Vec::new();
    file.take(LIMIT + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| "Cannot read workflow")?;
    if bytes.len() as u64 > LIMIT {
        return Err("Workflow exceeds 1 MiB".into());
    }
    let revision = format!("{:x}", Sha256::digest(&bytes));
    let source = String::from_utf8(bytes).map_err(|_| "Workflow must be UTF-8")?;
    Ok(Workflow {
        path: relative.to_string(),
        source,
        revision,
    })
}

pub fn read(root: &Path, path: &str) -> Result<Workflow, String> {
    let relative = valid_path(path)?;
    read_at(&resolve(root, relative, false)?, path)
}

pub fn save(
    root: &Path,
    path: &str,
    source: &str,
    expected: Option<&str>,
) -> Result<Workflow, String> {
    if source.len() as u64 > LIMIT {
        return Err("Workflow exceeds 1 MiB".into());
    }
    let relative = valid_path(path)?;
    let target = resolve(root, relative, true)?;
    match expected {
        Some(revision) => {
            let current = read_at(&target, path)
                .map_err(|_| "Workflow conflict: file changed or was removed")?;
            if current.revision != revision {
                return Err("Workflow conflict: file changed externally".into());
            }
        }
        None if target.exists() => return Err("Workflow conflict: file already exists".into()),
        None => (),
    }
    let parent = target.parent().ok_or("Invalid workflow path")?;
    let mut temporary =
        tempfile::NamedTempFile::new_in(parent).map_err(|_| "Cannot create temporary workflow")?;
    if expected.is_some() {
        let permissions = fs::metadata(&target)
            .map_err(|_| "Workflow conflict: file changed or was removed")?
            .permissions();
        temporary
            .as_file()
            .set_permissions(permissions)
            .map_err(|_| "Cannot preserve workflow permissions")?;
    }
    temporary
        .write_all(source.as_bytes())
        .and_then(|_| temporary.as_file().sync_all())
        .map_err(|_| "Cannot write workflow")?;
    // Recheck immediately before replacement, after all fallible preparation.
    resolve(root, relative, expected.is_none())?;
    if let Some(revision) = expected {
        if read_at(&target, path)
            .map_err(|_| "Workflow conflict: file changed or was removed")?
            .revision
            != revision
        {
            return Err("Workflow conflict: file changed externally".into());
        }
        temporary
            .persist(&target)
            .map_err(|_| "Cannot replace workflow")?;
    } else {
        temporary
            .persist_noclobber(&target)
            .map_err(|_| "Workflow conflict: file already exists or cannot be created")?;
    }
    Ok(Workflow {
        path: path.to_string(),
        source: source.to_string(),
        revision: format!("{:x}", Sha256::digest(source.as_bytes())),
    })
}

pub fn list(root: &Path) -> Result<WorkflowList, String> {
    list_with_limits(root, 32, 10_000)
}

fn list_with_limits(
    root: &Path,
    max_depth: usize,
    max_entries: usize,
) -> Result<WorkflowList, String> {
    fn visit(
        root: &Path,
        dir: &Path,
        paths: &mut Vec<String>,
        depth: usize,
        max_depth: usize,
        remaining: &mut usize,
    ) -> Result<(), String> {
        for entry in fs::read_dir(dir).map_err(|_| "Cannot list workflows")? {
            let entry = entry.map_err(|_| "Cannot list workflows")?;
            if *remaining == 0 {
                return Err("Workflow discovery exceeds the entry limit".into());
            }
            *remaining -= 1;
            let ty = entry.file_type().map_err(|_| "Cannot inspect workflow")?;
            if ty.is_symlink() {
                continue;
            }
            let path = entry.path();
            if ty.is_dir() {
                if depth >= max_depth {
                    return Err("Workflow discovery exceeds the directory depth limit".into());
                }
                visit(root, &path, paths, depth + 1, max_depth, remaining)?;
            } else if ty.is_file()
                && matches!(
                    path.extension().and_then(|s| s.to_str()),
                    Some("yaml" | "yml")
                )
            {
                if let Some(path) = path.strip_prefix(root).ok().and_then(|p| p.to_str()) {
                    paths.push(path.replace('\\', "/"));
                }
            }
        }
        Ok(())
    }
    let mut paths = Vec::new();
    let mut remaining = max_entries;
    let dir = root.join("workflows");
    match fs::symlink_metadata(&dir) {
        Ok(meta) if meta.is_dir() && !meta.file_type().is_symlink() => {
            visit(root, &dir, &mut paths, 0, max_depth, &mut remaining)?
        }
        Ok(_) => return Err("Workflows directory must be a directory inside the workspace".into()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => (),
        Err(_) => return Err("Cannot list workflows".into()),
    }
    paths.sort();
    Ok(WorkflowList { paths })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn discovery_reports_depth_and_entry_limits() {
        let root = tempfile::tempdir().unwrap();
        let workflows = root.path().join("workflows");
        fs::create_dir_all(workflows.join("a/b")).unwrap();
        fs::write(workflows.join("a/b/test.yaml"), "test").unwrap();
        assert!(list_with_limits(root.path(), 1, 10)
            .err()
            .unwrap()
            .contains("depth"));
        assert!(list_with_limits(root.path(), 10, 2)
            .err()
            .unwrap()
            .contains("entry"));
        assert_eq!(
            list_with_limits(root.path(), 2, 3).unwrap().paths,
            vec!["workflows/a/b/test.yaml"]
        );
    }

    #[test]
    fn create_read_overwrite_and_conflicts() {
        let root = tempfile::tempdir().unwrap();
        fs::create_dir(root.path().join("workflows")).unwrap();
        let created = save(root.path(), "workflows/a.yaml", "first", None).unwrap();
        assert_eq!(
            read(root.path(), "workflows/a.yaml").unwrap().source,
            "first"
        );
        assert_eq!(list(root.path()).unwrap().paths, vec!["workflows/a.yaml"]);
        assert!(save(root.path(), "workflows/a.yaml", "clobber", None).is_err());
        let updated = save(
            root.path(),
            "workflows/a.yaml",
            "second",
            Some(&created.revision),
        )
        .unwrap();
        assert_ne!(created.revision, updated.revision);
        fs::write(root.path().join("workflows/a.yaml"), "external").unwrap();
        assert!(save(
            root.path(),
            "workflows/a.yaml",
            "clobber",
            Some(&updated.revision)
        )
        .is_err());
        assert_eq!(
            fs::read_to_string(root.path().join("workflows/a.yaml")).unwrap(),
            "external"
        );
        assert!(save(
            root.path(),
            "workflows/missing.yaml",
            "clobber",
            Some(&updated.revision)
        )
        .is_err());
    }

    #[test]
    fn rejects_traversal_and_oversized_content_without_changing_file() {
        let root = tempfile::tempdir().unwrap();
        fs::write(root.path().join("a.yml"), "original").unwrap();
        let original = read(root.path(), "a.yml").unwrap();
        assert!(read(root.path(), "../a.yml").is_err());
        assert!(read(root.path(), "/a.yml").is_err());
        assert!(save(
            root.path(),
            "a.yml",
            &"x".repeat(LIMIT as usize + 1),
            Some(&original.revision)
        )
        .is_err());
        assert_eq!(
            fs::read_to_string(root.path().join("a.yml")).unwrap(),
            "original"
        );
        fs::write(
            root.path().join("large.yaml"),
            vec![b'x'; LIMIT as usize + 1],
        )
        .unwrap();
        assert!(read(root.path(), "large.yaml").is_err());
    }

    #[cfg(unix)]
    #[test]
    fn rejects_symlinks_and_skips_them_in_listing() {
        use std::os::unix::fs::symlink;
        let root = tempfile::tempdir().unwrap();
        let outside = tempfile::tempdir().unwrap();
        fs::create_dir(root.path().join("workflows")).unwrap();
        fs::write(outside.path().join("other.yaml"), "outside").unwrap();
        symlink(outside.path(), root.path().join("workflows/linked")).unwrap();
        symlink(
            outside.path().join("other.yaml"),
            root.path().join("workflows/link.yaml"),
        )
        .unwrap();
        assert!(read(root.path(), "workflows/link.yaml").is_err());
        assert!(save(root.path(), "workflows/linked/new.yaml", "bad", None).is_err());
        assert!(list(root.path()).unwrap().paths.is_empty());
        assert_eq!(
            fs::read_to_string(outside.path().join("other.yaml")).unwrap(),
            "outside"
        );
    }
}
