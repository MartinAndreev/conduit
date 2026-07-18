import { useEffect, useRef } from "react";
import { useKeyboard, useRenderer } from "@opentui/react";
import type { BoxRenderable } from "@opentui/core";
import { useTheme } from "./ThemeProvider.js";

interface ScrollableDiffChild {
  scrollY: number;
  readonly maxScrollY: number;
  readonly height?: number;
}

function scrollableChildren(root: unknown): ScrollableDiffChild[] {
  if (!root || typeof root !== "object") return [];
  const candidate = root as {
    scrollY?: unknown;
    maxScrollY?: unknown;
    getChildren?: () => unknown[];
  };
  const current =
    typeof candidate.scrollY === "number" &&
    typeof candidate.maxScrollY === "number"
      ? [candidate as ScrollableDiffChild]
      : [];
  const children =
    typeof candidate.getChildren === "function" ? candidate.getChildren() : [];
  return [...current, ...children.flatMap(scrollableChildren)];
}

interface SplitDiffProps {
  readonly diff: string | undefined;
  readonly height?: number | `${number}%`;
  readonly showLineNumbers?: boolean;
  readonly id?: string;
}

export function SplitDiff({
  diff,
  height = 12,
  showLineNumbers = true,
  id,
}: SplitDiffProps) {
  const renderer = useRenderer();
  const theme = useTheme();
  const containerRef = useRef<BoxRenderable | null>(null);
  const diffRef = useRef<unknown>(null);

  useKeyboard((event) => {
    const targets = scrollableChildren(diffRef.current);
    if (!targets.length) return;
    const page = Math.max(
      1,
      Math.max(...targets.map((target) => target.height ?? 12)) - 2,
    );
    for (const target of targets) {
      if (event.name === "up") target.scrollY = Math.max(0, target.scrollY - 1);
      else if (event.name === "down")
        target.scrollY = Math.min(target.maxScrollY, target.scrollY + 1);
      else if (event.name === "pageup")
        target.scrollY = Math.max(0, target.scrollY - page);
      else if (event.name === "pagedown")
        target.scrollY = Math.min(target.maxScrollY, target.scrollY + page);
      else if (event.name === "home") target.scrollY = 0;
      else if (event.name === "end") target.scrollY = target.maxScrollY;
    }
  });

  useEffect(() => {
    let disposed = false;
    const container = containerRef.current;
    if (!container || !diff) return;
    void import("@opentui/core").then(
      ({ DiffRenderable, RGBA, SyntaxStyle }) => {
        if (disposed) return;
        const style = SyntaxStyle.fromStyles({
          default: { fg: RGBA.fromHex(theme.text.strong) },
          string: { fg: RGBA.fromHex(theme.action.primary) },
          keyword: { fg: RGBA.fromHex(theme.status.error), bold: true },
        });
        const diffRenderable = new DiffRenderable(renderer, {
          id: id ?? `split-diff-${Date.now()}`,
          diff,
          view: "split",
          syncScroll: true,
          width: "100%",
          height,
          syntaxStyle: style,
          showLineNumbers,
          wrapMode: "none",
        });
        container.add(diffRenderable);
        diffRef.current = diffRenderable;
      },
    );
    return () => {
      disposed = true;
      if (diffRef.current) {
        container.remove(diffRef.current as never);
        (diffRef.current as { destroy(): void }).destroy();
        diffRef.current = null;
      }
    };
  }, [diff, renderer, height, showLineNumbers, theme]);

  if (!diff)
    return (
      <box width="100%" height={height}>
        <text
          content="Select a changed file to view its diff."
          fg={theme.text.muted}
        />
      </box>
    );
  return <box width="100%" height={height} ref={containerRef as never} />;
}
