// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PackageBuilder } from "./PackageBuilder";
const native = vi.hoisted(() => ({ invoke: vi.fn() }));
vi.mock("@tauri-apps/api/core", () => ({ invoke: native.invoke }));
afterEach(cleanup);
beforeEach(() => {
  localStorage.clear();
  native.invoke.mockReset();
  native.invoke.mockImplementation(async (command: string) => {
    if (command === "get_signing_identity") return { available: false };
    if (command === "create_signing_identity")
      return {
        available: true,
        publicKey: "public-only",
        fingerprint: "fingerprint-123",
      };
    throw new Error("Build failed: Go compiler unavailable");
  });
});
function open() {
  const onClose = vi.fn(),
    onPending = vi.fn();
  render(
    <PackageBuilder
      workspace="/project"
      workflows={["workflows/a.yaml", "workflows/b.yaml"]}
      onClose={onClose}
      onPending={onPending}
    />,
  );
  return { onClose, onPending };
}
it("blocks workflow names the deployed service cannot load before invoking the builder", async () => {
  const user = userEvent.setup();
  render(
    <PackageBuilder
      workspace="/project"
      workflows={["workflows/1-invalid.yaml"]}
      onClose={() => {}}
      onPending={() => {}}
    />,
  );
  await screen.findByText("No signing identity");
  await user.click(screen.getByRole("radio", { name: /Trusted local/ }));
  expect(screen.getByText(/filenames must.*starting with a letter/)).toBeTruthy();
  expect(
    (screen.getByRole("button", { name: "Build ZIP package" }) as HTMLButtonElement).disabled,
  ).toBe(true);
  expect(native.invoke).not.toHaveBeenCalledWith("build_linux_package", expect.anything());
  await user.click(screen.getByRole("checkbox", { name: "workflows/1-invalid.yaml" }));
  expect(
    (screen.getByRole("button", { name: "Build ZIP package" }) as HTMLButtonElement).disabled,
  ).toBe(false);
});
it("defaults to secure and requires both signing identity and an age public recipient", async () => {
  const user = userEvent.setup();
  open();
  await screen.findByText("No signing identity");
  const build = screen.getByRole("button", {
    name: "Build ZIP package",
  }) as HTMLButtonElement;
  expect(build.disabled).toBe(true);
  await user.click(
    screen.getByRole("button", { name: "Create signing identity" }),
  );
  await screen.findByText(/fingerprint-123/);
  expect(build.disabled).toBe(true);
  await user.type(
    screen.getByLabelText("Deployment age public recipient"),
    "AGE-SECRET-KEY-NEVER-ACCEPT",
  );
  expect(build.disabled).toBe(true);
  fireEvent.change(screen.getByLabelText("Deployment age public recipient"), {
    target: { value: `age1${"q".repeat(58)}` },
  });
  expect(build.disabled).toBe(false);
  await user.click(build);
  await screen.findByRole("alert");
  expect(native.invoke).toHaveBeenCalledWith("build_linux_package", {
    profile: {
      arch: "amd64",
      configPath: "config.json",
      workflows: ["workflows/a.yaml", "workflows/b.yaml"],
      installDir: "/opt/maiservice",
      serviceName: "maiservice",
      port: 3081,
      memoryMB: 650,
      goMemoryMB: 400,
      mode: "secure",
      recipient: `age1${"q".repeat(58)}`,
    },
  });
  expect(screen.queryByText("Package created")).toBeNull();
});
it("requires explicit trusted selection, validates settings and reports native failure without success", async () => {
  const user = userEvent.setup();
  open();
  await screen.findByText("No signing identity");
  await user.click(screen.getByRole("radio", { name: /Trusted local/ }));
  const build = screen.getByRole("button", {
    name: "Build ZIP package",
  }) as HTMLButtonElement;
  fireEvent.change(screen.getByLabelText("HTTP port"), {
    target: { value: "70000" },
  });
  expect(build.disabled).toBe(true);
  expect(screen.getByText(/Port must/)).toBeTruthy();
  fireEvent.change(screen.getByLabelText("HTTP port"), {
    target: { value: "3081" },
  });
  fireEvent.change(screen.getByLabelText("Configuration file"), {
    target: { value: "../outside.json" },
  });
  expect(build.disabled).toBe(true);
  fireEvent.change(screen.getByLabelText("Configuration file"), {
    target: { value: "deploy/config.json" },
  });
  await user.click(screen.getByRole("checkbox", { name: "workflows/b.yaml" }));
  await user.click(build);
  expect(
    await screen.findByText(/Build failed: Go compiler unavailable/),
  ).toBeTruthy();
  expect(native.invoke).toHaveBeenCalledWith("build_linux_package", {
    profile: expect.objectContaining({
      configPath: "deploy/config.json",
      mode: "trusted",
      workflows: ["workflows/a.yaml"],
      recipient: undefined,
    }),
  });
  expect(screen.queryByText("Package created")).toBeNull();
});
it("keeps a pending build visible, prevents duplicate submission and shows actual build results", async () => {
  let resolveBuild: (result: unknown) => void = () => {};
  native.invoke.mockImplementation((command: string) =>
    command === "get_signing_identity"
      ? Promise.resolve({ available: false })
      : new Promise((resolve) => {
          resolveBuild = resolve;
        }),
  );
  const user = userEvent.setup();
  const { onClose, onPending } = open();
  await screen.findByText("No signing identity");
  await user.click(screen.getByRole("radio", { name: /Trusted local/ }));
  await user.click(screen.getByRole("button", { name: "Build ZIP package" }));
  expect(onPending).toHaveBeenLastCalledWith(true);
  expect(
    (
      screen.getByRole("button", {
        name: "Close package builder",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(
    (screen.getByRole("button", { name: "Save profile" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.keyDown(window, { key: "Escape" });
  expect(onClose).not.toHaveBeenCalled();
  expect(screen.queryByText(/\d+%/)).toBeNull();
  await act(async () =>
    resolveBuild({
      path: "/project/dist/test.zip",
      sha256: "checksum123",
      arch: "arm64",
      mode: "trusted",
      files: ["bin/service", "manifest.json"],
      validation: { crossCompiled: true, runtimeTested: false },
    }),
  );
  expect(await screen.findByText("Package created")).toBeTruthy();
  expect(screen.getByText("/project/dist/test.zip")).toBeTruthy();
  expect(screen.getByText("checksum123")).toBeTruthy();
  expect(screen.getByText(/"runtimeTested": false/)).toBeTruthy();
  expect(onPending).toHaveBeenLastCalledWith(false);
});
it("persists only public profile inputs under the workspace and clears profiles", async () => {
  const user = userEvent.setup();
  open();
  await screen.findByText("No signing identity");
  fireEvent.change(screen.getByLabelText("Deployment age public recipient"), {
    target: { value: "AGE-SECRET-KEY-MUST-NOT-PERSIST" },
  });
  await user.type(screen.getByLabelText("Profile name"), "Production");
  await user.click(screen.getByRole("button", { name: "Save profile" }));
  const stored = JSON.parse(
    localStorage.getItem("mai-studio:linux-package-profiles:v1:/project")!,
  );
  expect(stored.Production.mode).toBe("secure");
  expect(stored.Production.recipient).toBeUndefined();
  expect(JSON.stringify(stored)).not.toContain("SECRET");
  await user.click(
    screen.getByRole("button", { name: "Clear saved profiles" }),
  );
  await waitFor(() =>
    expect(
      localStorage.getItem("mai-studio:linux-package-profiles:v1:/project"),
    ).toBe("{}"),
  );
});
