import { useLayoutEffect, useMemo, useState } from "react";
import { RunnerError } from "./api";
import type { RequestCache } from "./RequestCache";

type Result<T> = { data: T; error?: never } | { data?: never; error: RunnerError };

/** Selects the result from this visit to an identity, including after a retry. */
export function useCurrentRequest<T>(
  key: string | null,
  scope: string,
  load: () => Promise<T>,
  delay = 0,
  onSuccess?: (data: T) => void,
  cache?: RequestCache<T>,
) {
  const [attempt, setAttempt] = useState(0);
  const generation = useMemo(() => ({ key, cache }), [key, cache, attempt]);
  const [response, setResponse] = useState<{ generation: typeof generation; scope: string; result: Result<T> }>();
  useLayoutEffect(() => {
    const { key, cache } = generation;
    if (key === null) return;
    const hit = cache?.get(key);
    if (hit !== undefined) {
      setResponse({ generation, scope, result: { data: hit } });
      return;
    }
    let live = true;
    const timer = setTimeout(async () => {
      try {
        const data = await (cache ? cache.load(key, load) : load());
        if (!live) return;
        setResponse({ generation, scope, result: { data } });
        onSuccess?.(data);
      } catch (error) {
        if (!live) return;
        setResponse({
          generation,
          scope,
          result: { error: asRunnerError(error) },
        });
      }
    }, delay);
    return () => {
      live = false;
      clearTimeout(timer);
    };
    // The key and cache instance together identify all request parameters.
  }, [generation, delay]);

  const current = response?.generation === generation ? response.result : undefined;
  const pending = key !== null && !current;
  return {
    pending,
    data: current?.data,
    error: current?.error,
    previous: !current && response?.scope === scope ? response.result.data : undefined,
    retry: () => {
      if (key !== null) cache?.forget(key);
      setAttempt((value) => value + 1);
    },
  };
}

export function asRunnerError(error: unknown): RunnerError {
  return error instanceof RunnerError ? error : new RunnerError(error instanceof Error ? error.message : String(error));
}
