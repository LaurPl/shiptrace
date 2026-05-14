import { useEffect, useState } from "react";

type LoadFn<T> = () => Promise<T>;

interface State<T> {
  status: "loading" | "ok" | "error";
  data?: T;
  error?: string;
}

// useLoader is a minimal data-fetch hook. We deliberately don't pull in
// react-query: the dashboard pages each call one endpoint, and a
// dependency we'd never lean on otherwise isn't worth the build size.
export function useLoader<T>(fn: LoadFn<T>, deps: unknown[] = []): State<T> {
  const [state, setState] = useState<State<T>>({ status: "loading" });
  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    fn()
      .then((data) => {
        if (!cancelled) setState({ status: "ok", data });
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: "error", error: err.message });
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return state;
}

// LoaderBoundary wraps the common loading/error/empty surfaces so each
// view doesn't repeat the same 8 lines.
export function LoaderBoundary<T>({
  state,
  empty,
  children,
}: {
  state: State<T>;
  empty?: (data: T) => boolean;
  children: (data: T) => JSX.Element;
}) {
  if (state.status === "loading") {
    return <div className="loading">loading…</div>;
  }
  if (state.status === "error") {
    return <div className="error">error: {state.error}</div>;
  }
  if (state.data === undefined) {
    return <div className="empty">no data</div>;
  }
  if (empty && empty(state.data)) {
    return <div className="empty">no data in this window</div>;
  }
  return children(state.data);
}
