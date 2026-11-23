import { expect, it } from "vitest";
import { RequestCache } from "./RequestCache";

it("bounds retained JSON by UTF-8 bytes and evicts the least recently used result", async () => {
  const cache = new RequestCache<string>(8, () => true);
  await cache.load("a", async () => "é");
  await cache.load("b", async () => "é");
  expect(cache.get("a")).toBe("é");
  await cache.load("c", async () => "é");
  expect(cache.get("b")).toBeUndefined();
  expect(cache.get("a")).toBe("é");
  expect(await cache.load("large", async () => "too large")).toBe("too large");
  expect(cache.get("large")).toBeUndefined();
  expect(cache.get("c")).toBe("é");
});

it("shares in-flight requests and prevents a superseded request from replacing its retry", async () => {
  const cache = new RequestCache<string>(64, () => true);
  let finish!: (data: string) => void;
  const first = cache.load("a", () => new Promise((resolve) => { finish = resolve; }));
  expect(cache.load("a", async () => "duplicate")).toBe(first);
  cache.forget("a");
  await cache.load("a", async () => "fresh");
  finish("old");
  await first;
  expect(cache.get("a")).toBe("fresh");
});

it("allows another attempt after either a rejected request or an error in the returned trace", async () => {
  const cache = new RequestCache<string>(64, (data) => data !== "error");
  await expect(cache.load("a", async () => { throw Error("failed"); })).rejects.toThrow("failed");
  expect(await cache.load("a", async () => "error")).toBe("error");
  expect(await cache.load("a", async () => "recovered")).toBe("recovered");
});
