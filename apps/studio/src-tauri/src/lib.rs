use serde_json::{json, Value};
use std::{
    path::{Path, PathBuf},
    process::Stdio,
    sync::Mutex,
};
use tauri::State;
use tauri_plugin_dialog::DialogExt;
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    process::Command,
    time::{timeout, Duration},
};

struct Workspace {
    root: Mutex<Option<PathBuf>>,
    slots: tokio::sync::Semaphore,
}
impl Default for Workspace {
    fn default() -> Self {
        Self {
            root: Mutex::new(None),
            slots: tokio::sync::Semaphore::new(2),
        }
    }
}

#[tauri::command]
async fn choose_workspace(
    app: tauri::AppHandle,
    workspace: State<'_, Workspace>,
) -> Result<Value, String> {
    let folder =
        tauri::async_runtime::spawn_blocking(move || app.dialog().file().blocking_pick_folder())
            .await
            .map_err(|_| "Workspace picker failed")?;
    let Some(folder) = folder else {
        return Ok(Value::Null);
    };
    let path = folder
        .into_path()
        .map_err(|_| "Choose a local folder")?
        .canonicalize()
        .map_err(|_| "Cannot access the selected workspace")?;
    if !path.is_dir() {
        return Err("Workspace must be a directory".into());
    }
    *workspace.root.lock().map_err(|_| "Workspace lock failed")? = Some(path.clone());
    Ok(json!({"path": path}))
}

fn authorize(request: &mut Value, root: Option<&Path>) -> Result<(), String> {
    if request.get("version").and_then(Value::as_u64) != Some(1) {
        return Err("Unsupported runner protocol version".into());
    }
    let command = request
        .get("command")
        .and_then(Value::as_str)
        .ok_or("Missing command")?;
    if !matches!(
        command,
        "capabilities" | "validate" | "inspectWorkbook" | "run"
    ) {
        return Err("Unsupported runner command".into());
    }
    if command == "capabilities" {
        return Ok(());
    }
    let root = root.ok_or("Choose a workspace first")?;
    let params = request
        .get_mut("params")
        .and_then(Value::as_object_mut)
        .ok_or("Missing parameters")?;
    // The native picker, not frontend-supplied paths, establishes authority.
    params.insert("workspace".into(), json!(root));
    let check = |value: &Value| -> Result<(), String> {
        let relative = value.as_str().ok_or("File paths must be strings")?;
        let path = Path::new(relative);
        if path.is_absolute() {
            return Err("Use paths relative to the workspace".into());
        }
        let resolved = root
            .join(path)
            .canonicalize()
            .map_err(|_| "Input file does not exist")?;
        if !resolved.starts_with(root) || !resolved.is_file() {
            return Err("Input must be a file inside the selected workspace".into());
        }
        Ok(())
    };
    if let Some(path) = params.get("path") {
        check(path)?
    }
    if let Some(inputs) = params.get("inputs") {
        for path in inputs
            .as_object()
            .ok_or("Inputs must be an object")?
            .values()
        {
            check(path)?
        }
    }
    Ok(())
}

fn runner_path() -> Result<PathBuf, String> {
    // Release bundles place the sidecar beside the desktop executable.
    let name = if cfg!(windows) {
        "mai-runner.exe"
    } else {
        "mai-runner"
    };
    if !cfg!(debug_assertions) {
        return std::env::current_exe()
            .map_err(|_| "Cannot locate application")?
            .parent()
            .ok_or_else(|| "Cannot locate application directory".to_string())
            .map(|dir| dir.join(name));
    }
    // Development uses only the repository-owned target-specific sidecar.
    let directory = Path::new(env!("CARGO_MANIFEST_DIR")).join("binaries");
    let entries = std::fs::read_dir(directory).map_err(|_| "Build the Go sidecar first")?;
    let candidates: Vec<_> = entries
        .filter_map(Result::ok)
        .map(|entry| entry.path())
        .filter(|path| {
            path.file_name()
                .and_then(|name| name.to_str())
                .is_some_and(|name| name.starts_with("mai-runner-") && !name.ends_with(".sha256"))
        })
        .collect();
    if candidates.len() != 1 {
        return Err("Build exactly one host Go sidecar before desktop development".into());
    }
    Ok(candidates[0].clone())
}

#[tauri::command]
async fn runner_request(
    mut request: Value,
    workspace: State<'_, Workspace>,
) -> Result<Value, String> {
    let _slot = workspace
        .slots
        .try_acquire()
        .map_err(|_| "Two operations are already running; wait before starting another")?;
    let root = workspace
        .root
        .lock()
        .map_err(|_| "Workspace lock failed")?
        .clone();
    authorize(&mut request, root.as_deref())?;
    let mut bytes = serde_json::to_vec(&request).map_err(|_| "Invalid request")?;
    if bytes.len() > 1024 * 1024 {
        return Err("Request exceeds 1 MiB".into());
    }
    bytes.push(b'\n');
    let mut child = Command::new(runner_path()?)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .kill_on_drop(true)
        .spawn()
        .map_err(|_| "Cannot launch Go sidecar; rebuild it for this host")?;
    let mut stdin = child.stdin.take().ok_or("Cannot open runner input")?;
    stdin
        .write_all(&bytes)
        .await
        .map_err(|_| "Cannot send runner request")?;
    drop(stdin); // One request per process; EOF lets the runner exit cleanly.
    let output = timeout(Duration::from_secs(120), async {
        let stdout = child.stdout.take().ok_or("Cannot open runner output")?;
        let mut output = Vec::new();
        stdout
            .take(1024 * 1024 + 1)
            .read_to_end(&mut output)
            .await
            .map_err(|_| "Cannot read runner output")?;
        if output.len() > 1024 * 1024 {
            return Err("Runner response exceeds 1 MiB");
        }
        let status = child.wait().await.map_err(|_| "Runner process failed")?;
        if !status.success() {
            return Err("Runner exited unsuccessfully");
        }
        Ok(output)
    })
    .await
    .map_err(|_| "Run exceeded the 120-second desktop limit")??;
    let response: Value = serde_json::from_slice(&output).map_err(|_| "Invalid runner response")?;
    if response.get("version") != request.get("version") || response.get("id") != request.get("id")
    {
        return Err("Runner response identity mismatch".into());
    }
    Ok(response)
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .manage(Workspace::default())
        .invoke_handler(tauri::generate_handler![choose_workspace, runner_request])
        .run(tauri::generate_context!())
        .expect("failed to start Mai Studio");
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn rejects_paths_outside_workspace() {
        let root = std::env::temp_dir().join(format!("mai-host-test-{}", std::process::id()));
        let workspace = root.join("workspace");
        std::fs::create_dir_all(&workspace).unwrap();
        let outside = root.join("outside.xlsx");
        std::fs::write(&outside, b"data").unwrap();
        let mut request =
            json!({"version":1,"command":"inspectWorkbook","params":{"path":"../outside.xlsx"}});
        assert!(authorize(&mut request, Some(&workspace)).is_err());
        #[cfg(unix)]
        {
            std::os::unix::fs::symlink(&outside, workspace.join("linked.xlsx")).unwrap();
            request["params"]["path"] = json!("linked.xlsx");
            assert!(authorize(&mut request, Some(&workspace)).is_err());
        }
        std::fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn refuses_unselected_workspaces_and_unknown_commands() {
        assert!(authorize(&mut json!({"version":1,"command":"run","params":{}}), None).is_err());
        assert!(authorize(&mut json!({"version":1,"command":"shell"}), None).is_err());
        assert!(authorize(&mut json!({"version":1,"command":"capabilities"}), None).is_ok());
    }
}
