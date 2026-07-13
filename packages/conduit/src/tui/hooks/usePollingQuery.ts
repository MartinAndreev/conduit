import { useCallback, useEffect, useRef, useState } from "react";

export interface PollingQueryOptions<TQuery, TResult, TData> {
  readonly execute: (query: TQuery) => Promise<TResult>;
  readonly createQuery: () => TQuery;
  readonly project: (result: TResult) => TData;
  readonly enabled: boolean;
  readonly intervalMs: number;
}

export interface PollingQueryState<T> {
  readonly data: T | undefined;
  readonly loading: boolean;
  readonly error: string | null;
  readonly reload: () => void;
}

export function usePollingQuery<TQuery, TResult, TData>({
  execute,
  createQuery,
  project,
  enabled,
  intervalMs,
}: PollingQueryOptions<TQuery, TResult, TData>): PollingQueryState<TData> {
  const [data, setData] = useState<TData>();
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState<string | null>(null);
  const optionsRef = useRef({ execute, createQuery, project });
  const requestIdRef = useRef(0);
  const hasLoadedRef = useRef(false);

  optionsRef.current = { execute, createQuery, project };

  const reload = useCallback(() => {
    const requestId = ++requestIdRef.current;
    if (!hasLoadedRef.current) setLoading(true);
    setError(null);
    const options = optionsRef.current;
    void options
      .execute(options.createQuery())
      .then((result) => {
        if (requestId !== requestIdRef.current) return;
        setData(options.project(result));
        hasLoadedRef.current = true;
        setLoading(false);
      })
      .catch((reason: unknown) => {
        if (requestId !== requestIdRef.current) return;
        setError(reason instanceof Error ? reason.message : String(reason));
        setLoading(false);
      });
  }, []);

  useEffect(() => {
    if (!enabled) {
      requestIdRef.current += 1;
      setLoading(false);
      return;
    }
    reload();
    const timer = setInterval(reload, intervalMs);
    return () => {
      clearInterval(timer);
      requestIdRef.current += 1;
    };
  }, [enabled, intervalMs, reload]);

  return { data, loading, error, reload };
}
