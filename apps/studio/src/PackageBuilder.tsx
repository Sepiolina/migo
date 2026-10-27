import { useEffect, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import "./package-builder.css";

export type PackageProfile = {
  arch: "amd64" | "arm64";
  configPath: string;
  workflows: string[];
  installDir: string;
  serviceName: string;
  port: number;
  memoryMB: number;
  goMemoryMB: number;
  mode: "secure" | "trusted";
  recipient?: string;
};
type Identity = {
  available: boolean;
  publicKey?: string;
  fingerprint?: string;
};
type PackageResult = {
  path: string;
  sha256: string;
  arch: string;
  mode: string;
  files: string[];
  validation: unknown;
  signingPublicKey?: string;
};
type Props = {
  workspace: string;
  workflows: string[];
  configPath?: string;
  blocked?: boolean;
  onClose: () => void;
  onPending: (pending: boolean) => void;
};
const defaults: PackageProfile = {
  arch: "amd64",
  configPath: "config.json",
  workflows: [],
  installDir: "/opt/maiservice",
  serviceName: "maiservice",
  port: 3081,
  memoryMB: 650,
  goMemoryMB: 400,
  mode: "secure",
};
const publicRecipient = (value: string | undefined) =>
  /^age1[023456789acdefghjklmnpqrstuvwxyz]{58}$/.test(value?.trim() ?? "");
function readProfiles(value: unknown): Record<string, PackageProfile> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  return Object.fromEntries(
    Object.entries(value).flatMap(([name, candidate]) => {
      if (
        !candidate ||
        typeof candidate !== "object" ||
        Array.isArray(candidate)
      )
        return [];
      const saved = candidate as Record<string, unknown>;
      if (
        !["configPath", "installDir", "serviceName"].every(
          (key) => typeof saved[key] === "string",
        ) ||
        !["port", "memoryMB", "goMemoryMB"].every(
          (key) => typeof saved[key] === "number",
        ) ||
        !Array.isArray(saved.workflows) ||
        !saved.workflows.every((path) => typeof path === "string")
      )
        return [];
      // Whitelist public settings so stale or edited storage cannot add native-only fields.
      return [
        [
          name,
          {
            arch: saved.arch === "arm64" ? "arm64" : "amd64",
            mode: saved.mode === "trusted" ? "trusted" : "secure",
            configPath: saved.configPath,
            workflows: saved.workflows,
            installDir: saved.installDir,
            serviceName: saved.serviceName,
            port: saved.port,
            memoryMB: saved.memoryMB,
            goMemoryMB: saved.goMemoryMB,
            recipient:
              typeof saved.recipient === "string" &&
              publicRecipient(saved.recipient)
                ? saved.recipient.trim()
                : undefined,
          } as PackageProfile,
        ],
      ];
    }),
  );
}
const relativePath = (value: string) =>
  !!value &&
  !value.startsWith("/") &&
  !value.includes("\\") &&
  !value.split("/").some((part) => part === ".." || part === "." || !part);
export function packageProfileError(
  profile: PackageProfile,
  identity: Identity | undefined,
): string {
  if (
    !relativePath(profile.configPath) ||
    profile.workflows.some((path) => !relativePath(path))
  )
    return "Use workspace-relative configuration and workflow paths.";
  if (profile.configPath.split("/").at(-1) !== "config.json")
    return "Choose a configuration file named config.json.";
  if (profile.workflows.length > 100)
    return "Select at most 100 workflow overrides.";
  const names = profile.workflows.map((path) => path.split("/").at(-1)!);
  if (
    names.some((name) => !/^[a-z0-9][a-z0-9-]{0,39}\.yaml$/.test(name)) ||
    new Set(names).size !== names.length
  )
    return "Workflow override filenames must be unique, lowercase names ending in .yaml (letters, numbers and hyphens; up to 40 characters before .yaml).";
  if (!/^\/opt\/[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/.test(profile.installDir))
    return "Enter an installation folder directly under /opt, such as /opt/maiservice.";
  if (!/^[a-z0-9][a-z0-9-]{0,39}$/.test(profile.serviceName))
    return "Service name must use 1–40 lowercase letters, numbers or hyphens and start with a letter or number.";
  if (
    !Number.isInteger(profile.port) ||
    profile.port < 1 ||
    profile.port > 65535
  )
    return "Port must be a whole number between 1 and 65535.";
  if (
    !Number.isInteger(profile.memoryMB) ||
    !Number.isInteger(profile.goMemoryMB) ||
    profile.memoryMB < 64 ||
    profile.memoryMB > 65536 ||
    profile.goMemoryMB < 32 ||
    profile.memoryMB < profile.goMemoryMB
  )
    return "PM2 memory limit must be 64–65536 MB. Go memory target must be at least 32 MB and no greater than the PM2 limit.";
  if (profile.mode === "secure" && !identity?.available)
    return "Create a signing identity before building a secure package.";
  if (profile.mode === "secure" && !publicRecipient(profile.recipient))
    return "Paste the deployment machine’s age public recipient (age1…).";
  return "";
}
export function PackageBuilder({
  workspace,
  workflows,
  configPath = "config.json",
  blocked,
  onClose,
  onPending,
}: Props) {
  const dialogRef = useRef<HTMLElement>(null);
  const [profile, setProfile] = useState<PackageProfile>({
    ...defaults,
    configPath,
    workflows: [...workflows],
  });
  const [identity, setIdentity] = useState<Identity>();
  const [identityBusy, setIdentityBusy] = useState(true);
  const [pending, setPending] = useState(false);
  const pendingRef = useRef(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<PackageResult>();
  const [profiles, setProfiles] = useState<Record<string, PackageProfile>>({});
  const [profileName, setProfileName] = useState("");
  const storageKey = `mai-studio:linux-package-profiles:v1:${workspace}`;
  useEffect(() => {
    let active = true;
    void invoke<Identity>("get_signing_identity")
      .then((value) => {
        if (active) setIdentity(value);
      })
      .catch((error) => {
        if (active)
          setError(`Could not read signing identity: ${String(error)}`);
      })
      .finally(() => {
        if (active) setIdentityBusy(false);
      });
    try {
      const stored = JSON.parse(localStorage.getItem(storageKey) ?? "{}");
      setProfiles(readProfiles(stored));
    } catch {
      /* A broken saved preference must not prevent packaging. */
    }
    return () => {
      active = false;
    };
  }, [storageKey]);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    return () => previous?.focus();
  }, []);
  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Tab") {
        const items = Array.from(
          dialogRef.current?.querySelectorAll<HTMLElement>(
            "button:not(:disabled), input:not(:disabled), select:not(:disabled), summary",
          ) ?? [],
        );
        const first = items[0],
          last = items[items.length - 1];
        if (!first) {
          event.preventDefault();
          dialogRef.current?.focus();
        } else if (
          event.shiftKey &&
          (document.activeElement === first ||
            !dialogRef.current?.contains(document.activeElement))
        ) {
          event.preventDefault();
          last.focus();
        } else if (
          !event.shiftKey &&
          (document.activeElement === last ||
            !dialogRef.current?.contains(document.activeElement))
        ) {
          event.preventDefault();
          first.focus();
        }
      }
      if (event.key === "Escape") {
        event.preventDefault();
        if (!pendingRef.current && !identityBusy) onClose();
      }
    };
    window.addEventListener("keydown", keydown);
    return () => window.removeEventListener("keydown", keydown);
  }, [onClose, identityBusy]);
  const invalid = packageProfileError(profile, identity);
  const locked = pending || identityBusy;
  function update<K extends keyof PackageProfile>(
    key: K,
    value: PackageProfile[K],
  ) {
    setProfile((current) => ({ ...current, [key]: value }));
    setResult(undefined);
  }
  function persist(next: Record<string, PackageProfile>) {
    try {
      localStorage.setItem(storageKey, JSON.stringify(next));
      setProfiles(next);
      setError("");
    } catch (error) {
      setError(`Could not save profiles: ${String(error)}`);
    }
  }
  async function createIdentity() {
    setIdentityBusy(true);
    setError("");
    onPending(true);
    pendingRef.current = true;
    try {
      setIdentity(await invoke<Identity>("create_signing_identity"));
    } catch (error) {
      setError(String(error));
    } finally {
      setIdentityBusy(false);
      onPending(false);
      pendingRef.current = false;
    }
  }
  async function build() {
    if (pendingRef.current || locked || blocked || invalid) return;
    pendingRef.current = true;
    setPending(true);
    onPending(true);
    setError("");
    setResult(undefined);
    try {
      const submitted = {
        ...profile,
        recipient:
          profile.mode === "secure" ? profile.recipient?.trim() : undefined,
      };
      setResult(
        await invoke<PackageResult>("build_linux_package", {
          profile: submitted,
        }),
      );
    } catch (error) {
      setError(String(error));
    } finally {
      pendingRef.current = false;
      setPending(false);
      onPending(false);
    }
  }
  return (
    <div className="dialog-backdrop package-backdrop">
      <section
        ref={dialogRef}
        tabIndex={-1}
        className="package-builder"
        role="dialog"
        aria-modal="true"
        aria-labelledby="package-title"
        aria-busy={locked}
      >
        <div className="package-heading">
          <div>
            <div className="eyebrow">LINUX DEPLOYMENT</div>
            <h2 id="package-title">Build Linux package</h2>
            <p>
              Create an offline ZIP for your Linux machine. Source files stay in
              your workspace.
            </p>
          </div>
          <button
            autoFocus
            disabled={locked}
            onClick={onClose}
            aria-label="Close package builder"
          >
            Close
          </button>
        </div>
        <div className="package-body">
          <fieldset disabled={locked}>
            <legend>Deployment profile</legend>
            <div className="package-grid">
              <label>
                Saved profile
                <select
                  aria-label="Saved package profile"
                  value=""
                  onChange={(event) => {
                    const saved = profiles[event.target.value];
                    if (saved) {
                      setProfile({
                        ...defaults,
                        ...saved,
                        workflows: (saved.workflows ?? []).filter((path) =>
                          workflows.includes(path),
                        ),
                      });
                      setProfileName(event.target.value);
                      setResult(undefined);
                    }
                  }}
                >
                  <option value="">Choose a profile…</option>
                  {Object.keys(profiles).map((name) => (
                    <option key={name}>{name}</option>
                  ))}
                </select>
              </label>
              <label>
                Profile name
                <input
                  value={profileName}
                  onChange={(event) => setProfileName(event.target.value)}
                />
              </label>
            </div>
            <div className="package-actions">
              <button
                disabled={!profileName.trim()}
                onClick={() =>
                  persist({
                    ...profiles,
                    [profileName.trim()]: {
                      ...profile,
                      recipient: publicRecipient(profile.recipient)
                        ? profile.recipient?.trim()
                        : undefined,
                    },
                  })
                }
              >
                Save profile
              </button>
              <button
                disabled={!Object.keys(profiles).length}
                onClick={() => persist({})}
              >
                Clear saved profiles
              </button>
            </div>
            <p className="hint">
              Profiles are stored on this computer for this workspace. They
              contain paths, deployment settings and public keys only.
            </p>
            <div className="package-grid">
              <label>
                Linux architecture
                <select
                  value={profile.arch}
                  onChange={(event) =>
                    update("arch", event.target.value as PackageProfile["arch"])
                  }
                >
                  <option value="amd64">amd64 · Intel / AMD 64-bit</option>
                  <option value="arm64">arm64 · ARM 64-bit</option>
                </select>
              </label>
              <label>
                Configuration file
                <input
                  aria-label="Configuration file"
                  value={profile.configPath}
                  placeholder="config.json"
                  onChange={(event) => update("configPath", event.target.value)}
                />
                <span className="hint">
                  Workspace-relative saved configuration; included from disk.
                </span>
              </label>
            </div>
            <div className="package-workflows">
              <strong>Workflow overrides</strong>
              <p className="hint">
                Built-in workflows remain available. Selected saved files
                override built-ins or add endpoints; this selection does not
                restrict available endpoints. Save edits before building.
              </p>
              {workflows.length ? (
                workflows.map((path) => (
                  <label className="package-choice" key={path}>
                    <input
                      type="checkbox"
                      checked={profile.workflows.includes(path)}
                      onChange={(event) =>
                        update(
                          "workflows",
                          event.target.checked
                            ? [...profile.workflows, path]
                            : profile.workflows.filter((item) => item !== path),
                        )
                      }
                    />
                    {path}
                  </label>
                ))
              ) : (
                <p>No saved workflows found in this workspace.</p>
              )}
            </div>
            <div className="package-grid">
              <label>
                Installation folder
                <input
                  value={profile.installDir}
                  onChange={(event) => update("installDir", event.target.value)}
                />
              </label>
              <label>
                Service name
                <input
                  value={profile.serviceName}
                  onChange={(event) =>
                    update("serviceName", event.target.value)
                  }
                />
              </label>
              <label>
                HTTP port
                <input
                  type="number"
                  min="1"
                  max="65535"
                  value={profile.port}
                  onChange={(event) =>
                    update("port", Number(event.target.value))
                  }
                />
              </label>
              <label>
                PM2 restart memory limit (MB)
                <input
                  type="number"
                  min="64"
                  max="65536"
                  value={profile.memoryMB}
                  onChange={(event) =>
                    update("memoryMB", Number(event.target.value))
                  }
                />
              </label>
              <label>
                Go memory target (MB)
                <input
                  type="number"
                  min="32"
                  max={profile.memoryMB}
                  value={profile.goMemoryMB}
                  onChange={(event) =>
                    update("goMemoryMB", Number(event.target.value))
                  }
                />
              </label>
            </div>
            <p className="hint">
              Building requires Go on this computer and the service source in
              the workspace. Linux needs PM2 and permissions to install in this
              folder. Memory settings guide runtime limits; allow additional
              memory for the OS and other services.
            </p>
          </fieldset>
          <fieldset disabled={locked}>
            <legend>Package security</legend>
            <label className="package-choice">
              <input
                type="radio"
                name="package-security"
                checked={profile.mode === "secure"}
                onChange={() => update("mode", "secure")}
              />
              Secure · recommended
            </label>
            <p className="hint">
              Sign the package and encrypt its configuration for the deployment
              machine. Signing private keys stay in the operating system
              credential store.
            </p>
            <label className="package-choice">
              <input
                type="radio"
                name="package-security"
                checked={profile.mode === "trusted"}
                onChange={() => update("mode", "trusted")}
              />
              Trusted local · plaintext configuration
            </label>
            <p className="hint">
              Choose only for a trusted local deployment. Configuration and its
              secrets are readable inside the ZIP.
            </p>
            {profile.mode === "secure" && (
              <>
                <div className="package-identity">
                  <strong>
                    {identityBusy
                      ? "Reading signing identity…"
                      : identity?.available
                        ? "Signing identity ready"
                        : "No signing identity"}
                  </strong>
                  {identity?.fingerprint && (
                    <p>
                      Fingerprint: <code>{identity.fingerprint}</code>
                    </p>
                  )}
                  {identity?.publicKey && (
                    <details>
                      <summary>Signing public key</summary>
                      <pre>{identity.publicKey}</pre>
                    </details>
                  )}
                  <button onClick={() => void createIdentity()}>
                    {identity?.available
                      ? "Use existing signing identity"
                      : "Create signing identity"}
                  </button>
                </div>
                <label>
                  Deployment age public recipient
                  <input
                    aria-label="Deployment age public recipient"
                    placeholder="age1…"
                    value={profile.recipient ?? ""}
                    onChange={(event) =>
                      update("recipient", event.target.value)
                    }
                    autoComplete="off"
                    spellCheck={false}
                  />
                  <span className="hint">
                    Paste only the public recipient generated on the Linux
                    machine. Keep its private identity on that machine.
                  </span>
                </label>
              </>
            )}
          </fieldset>
          {blocked && (
            <p role="alert">
              Save your workflow and apply or discard the argument draft before
              packaging.
            </p>
          )}
          {!pending && invalid && <p className="hint">{invalid}</p>}
          {error && (
            <p role="alert" className="package-error">
              {error}
            </p>
          )}
          {pending && (
            <p role="status">
              Building and validating the Linux package… This may take several
              minutes. Keep this window open; cancellation is unavailable.
            </p>
          )}
          {result && (
            <section className="package-result" aria-label="Package result">
              <h3>Package created</h3>
              <p>
                ZIP: <code>{result.path}</code>
              </p>
              <p>
                SHA-256: <code>{result.sha256}</code>
              </p>
              <p>
                Architecture: {result.arch} · Mode: {result.mode}
              </p>
              <details open>
                <summary>Included files ({result.files.length})</summary>
                <ul>
                  {result.files.map((file) => (
                    <li key={file}>
                      <code>{file}</code>
                    </li>
                  ))}
                </ul>
              </details>
              <h4>Validation reported by the builder</h4>
              <pre>
                {typeof result.validation === "string"
                  ? result.validation
                  : JSON.stringify(result.validation, null, 2)}
              </pre>
              {result.signingPublicKey && (
                <details>
                  <summary>Signing public key</summary>
                  <pre>{result.signingPublicKey}</pre>
                </details>
              )}
              <p className="hint">
                A package build does not prove runtime behavior on your Linux
                machine. Cross-compiled ARM packages require testing on the
                target device.
              </p>
            </section>
          )}
        </div>
        <div className="package-footer">
          <span>Workspace: {workspace}</span>
          <button
            className="primary"
            disabled={locked || !!blocked || !!invalid}
            onClick={() => void build()}
          >
            {pending ? "Building package…" : "Build ZIP package"}
          </button>
        </div>
      </section>
    </div>
  );
}
