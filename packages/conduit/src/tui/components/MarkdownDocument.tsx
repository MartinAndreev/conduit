import { useEffect, useRef } from "react";
import { useRenderer } from "@opentui/react";
import type {
  BoxRenderable,
  MarkdownRenderable,
  ScrollBoxRenderable,
} from "@opentui/core";

export function MarkdownDocument({ content }: { readonly content: string }) {
  const renderer = useRenderer();
  const containerRef = useRef<BoxRenderable | null>(null);
  const markdownRef = useRef<MarkdownRenderable | null>(null);
  const scrollRef = useRef<ScrollBoxRenderable | null>(null);
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    let cancelled = false;
    void import("@opentui/core").then(
      ({ MarkdownRenderable, RGBA, ScrollBoxRenderable, SyntaxStyle }) => {
        if (cancelled) return;
        const syntaxStyle = SyntaxStyle.fromStyles({
          "markup.heading.1": { fg: RGBA.fromHex("#8FB6A0"), bold: true },
          "markup.heading.2": { fg: RGBA.fromHex("#D8C28B"), bold: true },
          "markup.list": { fg: RGBA.fromHex("#D8D5C8") },
          default: { fg: RGBA.fromHex("#D8D5C8") },
        });
        const scrollbox = new ScrollBoxRenderable(renderer, {
          id: "feature-packet-scrollbox",
          width: "100%",
          height: "100%",
          scrollY: true,
          scrollbarOptions: {
            showArrows: true,
            trackOptions: {
              foregroundColor: "#8FB6A0",
              backgroundColor: "#2B332A",
            },
          },
        });
        const markdown = new MarkdownRenderable(renderer, {
          id: "feature-packet-markdown",
          width: "100%",
          height: "auto",
          content,
          syntaxStyle,
          conceal: true,
          tableOptions: {
            style: "grid",
            widthMode: "full",
            wrapMode: "word",
            cellPadding: 1,
            borders: true,
            outerBorder: true,
            borderColor: "#8B8B8B",
          },
        });
        scrollbox.add(markdown);
        container.add(scrollbox);
        scrollRef.current = scrollbox;
        markdownRef.current = markdown;
      },
    );
    return () => {
      cancelled = true;
      if (scrollRef.current) {
        container.remove(scrollRef.current);
        scrollRef.current.destroy();
        scrollRef.current = null;
      }
      if (markdownRef.current) {
        markdownRef.current.destroy();
        markdownRef.current = null;
      }
    };
  }, [content, renderer]);
  return <box ref={containerRef} width="100%" height="100%" />;
}
