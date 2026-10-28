use mai_studio_workspace as workflows;

use base64::{engine::general_purpose::STANDARD, Engine as _};
use ed25519_dalek::SigningKey;
use serde::Serialize;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
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
use zeroize::{Zeroize, Zeroizing};

mod runner_process;

struct Workspace {
    root: Mutex<Option<PathBuf>>,
    slots: tokio::sync::Semaphore,
    build_slot: tokio::sync::Semaphore,
    identity_lock: Mutex<()>,
}
impl Default for Workspace {
    fn default() -> Self {
        Self {
            root: Mutex::new(None),
            slots: tokio::sync::Semaphore::new(2),
            build_slot: tokio::sync::Semaphore::new(1),
            identity_lock: Mutex::new(()),
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

#[tauri::command]
fn list_workflows(workspace: State<'_, Workspace>) -> Result<workflows::WorkflowList, String> {
    let root = workspace.root.lock().map_err(|_| "Workspace lock failed")?;
    workflows::list(root.as_deref().ok_or("Choose a workspace first")?)
}

#[tauri::command]
fn read_workflow(
    path: String,
    workspace: State<'_, Workspace>,
) -> Result<workflows::Workflow, String> {
    let root = workspace.root.lock().map_err(|_| "Workspace lock failed")?;
    workflows::read(root.as_deref().ok_or("Choose a workspace first")?, &path)
}

#[tauri::command]
fn save_workflow(
    path: String,
    source: String,
    expected_revision: Option<String>,
    workspace: State<'_, Workspace>,
) -> Result<workflows::Workflow, String> {
    let root = workspace.root.lock().map_err(|_| "Workspace lock failed")?;
    workflows::save(
        root.as_deref().ok_or("Choose a workspace first")?,
        &path,
        &source,
        expected_revision.as_deref(),
    )
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
    invoke_runner(
        request,
        Duration::from_secs(120),
        "Run exceeded the 120-second desktop limit",
    )
    .await
}

async fn invoke_runner(
    request: Value,
    limit: Duration,
    timeout_message: &str,
) -> Result<Value, String> {
    invoke_runner_at(runner_path()?, request, limit, timeout_message).await
}

async fn invoke_runner_at(
    runner: PathBuf,
    mut request: Value,
    limit: Duration,
    timeout_message: &str,
) -> Result<Value, String> {
    let mut bytes = Zeroizing::new(serde_json::to_vec(&request).map_err(|_| "Invalid request")?);
    if let Some(build) = request
        .pointer_mut("/params/build")
        .and_then(Value::as_object_mut)
    {
        if let Some(Value::String(mut secret)) = build.remove("signingKey") {
            secret.zeroize();
        }
    }
    if bytes.len() > 1024 * 1024 {
        return Err("Request exceeds 1 MiB".into());
    }
    bytes.push(b'\n');
    let mut command = Command::new(runner);
    command
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .kill_on_drop(true);
    runner_process::configure(&mut command);
    let mut child = command
        .spawn()
        .map_err(|_| "Cannot launch Go sidecar; rebuild it for this host")?;
    let _process_tree =
        runner_process::guard(&child).map_err(|_| "Cannot configure Go sidecar process cleanup")?;
    let output = timeout(limit, async {
        let mut stdin = child.stdin.take().ok_or("Cannot open runner input")?;
        stdin
            .write_all(&bytes)
            .await
            .map_err(|_| "Cannot send runner request")?;
        bytes.zeroize();
        drop(stdin); // One request per process; EOF lets the runner exit cleanly.
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
    .map_err(|_| timeout_message)??;
    let response: Value = serde_json::from_slice(&output).map_err(|_| "Invalid runner response")?;
    if response.get("version") != request.get("version") || response.get("id") != request.get("id")
    {
        return Err("Runner response identity mismatch".into());
    }
    Ok(response)
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct SigningIdentity {
    available: bool,
    public_key: Option<String>,
    fingerprint: Option<String>,
}

fn credential() -> Result<keyring::Entry, String> {
    // Explicit backends are configured in Cargo.toml; unsupported targets must
    // never fall back to keyring's in-memory mock implementation.
    if !cfg!(any(
        target_os = "linux",
        target_os = "macos",
        target_os = "windows"
    )) {
        return Err(
            "Secure signing is unavailable on this platform; use trusted mode explicitly".into(),
        );
    }
    keyring::Entry::new("org.mai.studio", "linux-package-signing-v1")
        .map_err(|_| "Cannot access the OS credential store; unlock or configure it, or use trusted mode explicitly".into())
}

fn load_signing_key() -> Result<Option<SigningKey>, String> {
    let mut secret = match credential()?.get_secret() {
        Ok(secret) => secret,
        Err(keyring::Error::NoEntry) => return Ok(None),
        Err(_) => return Err("Cannot access the OS credential store; unlock or configure it, or use trusted mode explicitly".into()),
    };
    let key = <[u8; 32]>::try_from(secret.as_slice()).map(|bytes| SigningKey::from_bytes(&bytes));
    secret.zeroize();
    key.map(Some).map_err(|_| {
        "The stored signing identity is invalid; repair the OS credential store".into()
    })
}

fn identity(key: Option<&SigningKey>) -> SigningIdentity {
    match key {
        Some(key) => {
            let public = key.verifying_key().to_bytes();
            SigningIdentity {
                available: true,
                public_key: Some(STANDARD.encode(public)),
                fingerprint: Some(format!("{:x}", Sha256::digest(public))),
            }
        }
        None => SigningIdentity {
            available: false,
            public_key: None,
            fingerprint: None,
        },
    }
}

#[tauri::command]
fn get_signing_identity(workspace: State<'_, Workspace>) -> Result<SigningIdentity, String> {
    let _guard = workspace
        .identity_lock
        .lock()
        .map_err(|_| "Signing identity lock failed")?;
    Ok(identity(load_signing_key()?.as_ref()))
}

#[tauri::command]
fn create_signing_identity(workspace: State<'_, Workspace>) -> Result<SigningIdentity, String> {
    let _guard = workspace
        .identity_lock
        .lock()
        .map_err(|_| "Signing identity lock failed")?;
    if let Some(key) = load_signing_key()? {
        return Ok(identity(Some(&key)));
    }
    let key = SigningKey::generate(&mut rand_core::OsRng);
    let mut secret = key.to_bytes();
    let result = credential()?.set_secret(&secret);
    secret.zeroize();
    result.map_err(|_| "Cannot save the signing identity in the OS credential store; unlock or configure it, or use trusted mode explicitly")?;
    // Read back to ensure the store persisted the identity before reporting success.
    let stored =
        load_signing_key()?.ok_or("OS credential store did not retain the signing identity")?;
    if stored.verifying_key() != key.verifying_key() {
        return Err("OS credential store returned a different signing identity".into());
    }
    Ok(identity(Some(&stored)))
}

fn check_build_path(root: &Path, value: &Value) -> Result<(), String> {
    let path = Path::new(value.as_str().ok_or("Build paths must be strings")?);
    if path.as_os_str().is_empty()
        || path
            .components()
            .any(|part| !matches!(part, std::path::Component::Normal(_)))
    {
        return Err("Use workspace-relative build paths without traversal".into());
    }
    let resolved = root
        .join(path)
        .canonicalize()
        .map_err(|_| "Build input file does not exist")?;
    if !resolved.starts_with(root) || !resolved.is_file() {
        return Err("Build input must be a file inside the selected workspace".into());
    }
    Ok(())
}

#[tauri::command]
async fn build_linux_package(
    mut profile: Value,
    workspace: State<'_, Workspace>,
) -> Result<Value, String> {
    let _slot = workspace
        .build_slot
        .try_acquire()
        .map_err(|_| "A package build is already running")?;
    let root = workspace
        .root
        .lock()
        .map_err(|_| "Workspace lock failed")?
        .clone()
        .ok_or("Choose a workspace first")?;
    let build = profile.as_object_mut().ok_or("Invalid build profile")?;
    // Strictly whitelist the frontend profile; credentials and runner controls are native-only.
    let allowed = [
        "arch",
        "configPath",
        "workflows",
        "installDir",
        "serviceName",
        "port",
        "memoryMB",
        "goMemoryMB",
        "mode",
        "recipient",
    ];
    if build.keys().any(|key| !allowed.contains(&key.as_str())) {
        return Err("Unexpected build profile field".into());
    }
    check_build_path(
        &root,
        build
            .get("configPath")
            .ok_or("Choose a configuration file")?,
    )?;
    let paths = build
        .get("workflows")
        .and_then(Value::as_array)
        .ok_or("Choose workflows")?;
    for path in paths {
        check_build_path(&root, path)?;
    }
    let mode = build
        .get("mode")
        .and_then(Value::as_str)
        .unwrap_or("secure");
    let mut encoded_secret = None;
    match mode {
        "secure" => {
            let key = load_signing_key()?.ok_or("Create a signing identity in the OS credential store before building a secure package")?;
            let mut private = key.to_keypair_bytes();
            let encoded = STANDARD.encode(&private);
            private.zeroize();
            build.insert("mode".into(), json!("secure"));
            build.insert("signingKey".into(), json!(encoded));
            encoded_secret = Some(Zeroizing::new(encoded));
        }
        "trusted" => (),
        _ => return Err("Choose secure or trusted package mode".into()),
    }
    let mut request = json!({"version": 1, "id": "build-linux-package", "command": "buildPackage", "params": {"workspace": root}});
    // Move the credential-bearing profile; json!(profile) would clone it and
    // leave another signing-key string outside invoke_runner's cleanup.
    request["params"]["build"] = profile;
    let response = invoke_runner(
        request,
        Duration::from_secs(15 * 60),
        "Package build exceeded the 15-minute desktop limit",
    )
    .await?;
    // Never relay a credential if the sidecar includes request data in its response.
    let mut serialized = serde_json::to_string(&response).map_err(|_| "Invalid runner response")?;
    if let Some(secret) = encoded_secret {
        serialized = serialized.replace(secret.as_str(), "[redacted]");
    }
    let response: Value =
        serde_json::from_str(&serialized).map_err(|_| "Invalid runner response")?;
    if response.get("ok").and_then(Value::as_bool) == Some(true) {
        response
            .get("result")
            .cloned()
            .ok_or_else(|| "Runner returned no package result".into())
    } else {
        Err(response
            .get("error")
            .and_then(|error| error.get("message"))
            .and_then(Value::as_str)
            .unwrap_or("Package build failed")
            .to_string())
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .manage(Workspace::default())
        .invoke_handler(tauri::generate_handler![
            choose_workspace,
            runner_request,
            list_workflows,
            read_workflow,
            save_workflow,
            get_signing_identity,
            create_signing_identity,
            build_linux_package
        ])
        .run(tauri::generate_context!())
        .expect("failed to start Mai Studio");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(windows)]
    #[test]
    fn windows_runner_job_kills_descendants() {
        fn encoded_script(script: &str) -> String {
            let bytes: Vec<u8> = script.encode_utf16().flat_map(u16::to_le_bytes).collect();
            STANDARD.encode(bytes)
        }
        fn powershell_literal(path: &Path) -> String {
            path.to_string_lossy().replace('\'', "''")
        }

        let dir = tempfile::tempdir().unwrap();
        let ready = dir.path().join("ready");
        let marker = dir.path().join("survived");
        let child_script = format!(
            "Set-Content -LiteralPath '{}' -Value ready; Start-Sleep -Seconds 3; Set-Content -LiteralPath '{}' -Value survived",
            powershell_literal(&ready), powershell_literal(&marker)
        );
        let parent_script = format!(
            "$null = [Console]::In.ReadLine(); Start-Process -FilePath powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile', '-NonInteractive', '-EncodedCommand', '{}'; Start-Sleep -Seconds 10",
            encoded_script(&child_script)
        );

        tauri::async_runtime::block_on(async {
            let mut command = Command::new("powershell.exe");
            command
                .args([
                    "-NoProfile",
                    "-NonInteractive",
                    "-EncodedCommand",
                    &encoded_script(&parent_script),
                ])
                .stdin(Stdio::piped())
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .kill_on_drop(true);
            runner_process::configure(&mut command);
            let mut child = command.spawn().expect("start PowerShell runner");
            let guard = runner_process::guard(&child).expect("assign runner to Job Object");
            child
                .stdin
                .take()
                .unwrap()
                .write_all(b"start\n")
                .await
                .unwrap();

            timeout(Duration::from_secs(5), async {
                while !ready.exists() {
                    tokio::time::sleep(Duration::from_millis(50)).await;
                }
            })
            .await
            .expect("descendant did not start");
            drop(guard);
            let _ = child.wait().await;
            tokio::time::sleep(Duration::from_millis(3500)).await;
        });
        assert!(
            !marker.exists(),
            "runner descendant survived Job Object cleanup"
        );
    }
    #[cfg(unix)]
    #[test]
    fn timed_out_runner_kills_descendants() {
        use std::os::unix::fs::PermissionsExt;
        let dir = tempfile::tempdir().unwrap();
        let runner = dir.path().join("runner");
        let marker = dir.path().join("survived");
        std::fs::write(
            &runner,
            format!(
                "#!/bin/sh\n(sleep 1; touch '{}') &\nwait\n",
                marker.display()
            ),
        )
        .unwrap();
        std::fs::set_permissions(&runner, std::fs::Permissions::from_mode(0o700)).unwrap();
        let result = tauri::async_runtime::block_on(invoke_runner_at(
            runner,
            json!({"version": 1, "id": "test"}),
            Duration::from_millis(200),
            "timed out",
        ));
        assert_eq!(result.unwrap_err(), "timed out");
        std::thread::sleep(Duration::from_millis(1200));
        assert!(!marker.exists(), "runner descendant survived timeout");
    }

    #[cfg(unix)]
    #[test]
    fn cancelled_runner_kills_descendants() {
        use std::os::unix::fs::PermissionsExt;
        let dir = tempfile::tempdir().unwrap();
        let runner = dir.path().join("runner");
        let marker = dir.path().join("survived");
        std::fs::write(
            &runner,
            format!(
                "#!/bin/sh\n(sleep 1; touch '{}') &\nwait\n",
                marker.display()
            ),
        )
        .unwrap();
        std::fs::set_permissions(&runner, std::fs::Permissions::from_mode(0o700)).unwrap();
        tauri::async_runtime::block_on(async {
            let task = tokio::spawn(async move {
                invoke_runner_at(
                    runner,
                    json!({"version": 1, "id": "test"}),
                    Duration::from_secs(10),
                    "timed out",
                )
                .await
            });
            tokio::time::sleep(Duration::from_millis(200)).await;
            task.abort();
            let _ = task.await;
            tokio::time::sleep(Duration::from_millis(1200)).await;
        });
        assert!(!marker.exists(), "runner descendant survived cancellation");
    }

    #[cfg(unix)]
    #[test]
    fn stalled_runner_input_obeys_timeout() {
        use std::os::unix::fs::PermissionsExt;
        let dir = tempfile::tempdir().unwrap();
        let runner = dir.path().join("runner");
        std::fs::write(&runner, "#!/bin/sh\nexec sleep 10\n").unwrap();
        std::fs::set_permissions(&runner, std::fs::Permissions::from_mode(0o700)).unwrap();
        let request = json!({"version": 1, "id": "test", "params": {"build": {
            "signingKey": "secret", "data": "a".repeat(500_000)
        }}});
        let start = std::time::Instant::now();
        let result = tauri::async_runtime::block_on(invoke_runner_at(
            runner,
            request,
            Duration::from_millis(100),
            "timed out",
        ));
        assert_eq!(result.unwrap_err(), "timed out");
        assert!(start.elapsed() < Duration::from_secs(3));
    }

    #[cfg(target_os = "windows")]
    #[test]
    fn windows_credential_store_round_trip() {
        struct Cleanup(keyring::Entry);
        impl Drop for Cleanup {
            fn drop(&mut self) {
                let _ = self.0.delete_credential();
            }
        }
        let unique = format!(
            "test-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .expect("system clock before epoch")
                .as_nanos()
        );
        let service = "org.mai.studio.tests";
        let entry = keyring::Entry::new(service, &unique)
            .expect("Windows Credential Manager entry unavailable");
        let _cleanup = Cleanup(entry);
        let secret = b"synthetic-credential-store-round-trip";
        _cleanup
            .0
            .set_secret(secret)
            .expect("Windows Credential Manager failed to store test credential");
        let fresh = keyring::Entry::new(service, &unique)
            .expect("Windows Credential Manager entry unavailable on reopen");
        assert_eq!(
            fresh
                .get_secret()
                .expect("Windows Credential Manager failed to retrieve test credential"),
            secret
        );
    }

    #[test]
    fn signing_identity_uses_go_ed25519_keypair_format() {
        use ed25519_dalek::{Signer, Verifier};
        let key = SigningKey::from_bytes(&[7; 32]);
        let pair = key.to_keypair_bytes();
        assert_eq!(&pair[..32], &key.to_bytes());
        assert_eq!(&pair[32..], &key.verifying_key().to_bytes());
        let public = identity(Some(&key));
        assert_eq!(public.public_key.unwrap(), STANDARD.encode(&pair[32..]));
        assert_eq!(
            public.fingerprint.unwrap(),
            format!("{:x}", Sha256::digest(&pair[32..]))
        );
        key.verifying_key()
            .verify(b"package", &key.sign(b"package"))
            .unwrap();
        assert!(!identity(None).available);
    }

    #[test]
    fn build_inputs_are_scoped_to_selected_workspace() {
        let root = tempfile::tempdir().unwrap();
        let canonical_root = root.path().canonicalize().unwrap();
        std::fs::write(root.path().join("config.yaml"), b"config").unwrap();
        assert!(check_build_path(&canonical_root, &json!("config.yaml")).is_ok());
        assert!(check_build_path(&canonical_root, &json!("../config.yaml")).is_err());
        assert!(check_build_path(&canonical_root, &json!("/etc/passwd")).is_err());
        #[cfg(unix)]
        {
            std::os::unix::fs::symlink("/etc/passwd", root.path().join("link")).unwrap();
            assert!(check_build_path(&canonical_root, &json!("link")).is_err());
        }
    }
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
