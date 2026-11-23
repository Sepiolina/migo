// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { request, RunnerError } from "./api";

afterEach(() => vi.unstubAllGlobals());

it("retains the runner's error code and message", async () => {
  vi.stubGlobal("__maiBridge", {
    invoke: async () => ({ ok: false, error: { code: "TRACE", message: "Preview failed." } }),
  });
  const failure = request("trace", {});
  await expect(failure).rejects.toBeInstanceOf(RunnerError);
  await expect(failure).rejects.toMatchObject({ code: "TRACE", message: "Preview failed." });
});

it("cancels a reservation when interrupted before the host can start tests", async () => {
  let finishPreparation!: () => void;
  const preparation = new Promise<void>((resolve) => { finishPreparation = resolve; });
  const invoke = vi.fn(async (command: string, _args: unknown) => {
    if (command === "runner_prepare_tests") await preparation;
  });
  vi.stubGlobal("__maiBridge", { invoke });
  const controller = new AbortController();
  const run = request("runTests", {}, controller.signal);
  const outcome = expect(run).rejects.toMatchObject({ name: "AbortError" });
  controller.abort();
  finishPreparation();
  await outcome;
  const reservation = invoke.mock.calls[0][1] as { id: string };
  expect(invoke).toHaveBeenCalledTimes(2);
  expect(invoke).toHaveBeenLastCalledWith("runner_cancel_tests", reservation);
});

it("asks the host to stop an active test run and ignores a late response", async () => {
  let finishRun!: (value: { ok: true; result: string }) => void;
  const reply = new Promise<{ ok: true; result: string }>((resolve) => { finishRun = resolve; });
  const invoke = vi.fn(async (command: string, _args: unknown) => {
    if (command === "runner_request") return reply;
  });
  vi.stubGlobal("__maiBridge", { invoke });
  const controller = new AbortController();
  const run = request("runTests", { path: "workflows/orders.yaml" }, controller.signal);
  const outcome = expect(run).rejects.toMatchObject({ name: "AbortError" });
  await vi.waitFor(() => expect(invoke).toHaveBeenCalledTimes(2));
  const reservation = invoke.mock.calls[0][1] as { id: string };
  expect(invoke).toHaveBeenNthCalledWith(2, "runner_request", {
    cancellable: true,
    request: {
      version: 1,
      id: reservation.id,
      command: "runTests",
      params: { path: "workflows/orders.yaml" },
    },
  });
  controller.abort();
  await outcome;
  expect(invoke).toHaveBeenLastCalledWith("runner_cancel_tests", reservation);
  finishRun({ ok: true, result: "late result" });
});
