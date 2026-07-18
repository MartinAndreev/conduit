import { useEffect, useRef } from "react";
import { useRenderer } from "@opentui/react";
import type {
  BoxRenderable,
  ScrollBoxRenderable,
  TextRenderable,
} from "@opentui/core";
import type { RunnerEvent } from "@domains/runs/types/runner-events.js";
import { formatEventDescription } from "@tui/helpers/event-presentation.js";
import { useTheme } from "./ThemeProvider.js";

interface AgentEventLogProps {
  readonly events: readonly RunnerEvent[];
  readonly scrollOffset: number;
  readonly expandedEventIndex: number | null;
  readonly transcriptExpanded: boolean;
}

function eventLogContent(props: AgentEventLogProps): string {
  if (props.events.length === 0) return "No activity events yet";
  return props.events
    .flatMap((event, index) => {
      const marker = index === props.expandedEventIndex ? "›" : " ";
      const lines = [`${marker}${formatEventDescription(event)}`];
      if (props.transcriptExpanded && props.expandedEventIndex === index)
        lines.push(`  ${JSON.stringify(event.payload)}`);
      return lines;
    })
    .join("\n");
}

export function AgentEventLog(props: AgentEventLogProps) {
  const renderer = useRenderer();
  const theme = useTheme();
  const containerRef = useRef<BoxRenderable | null>(null);
  const scrollRef = useRef<ScrollBoxRenderable | null>(null);
  const textRef = useRef<TextRenderable | null>(null);
  const content = eventLogContent(props);
  const contentRef = useRef(content);
  const scrollOffsetRef = useRef(props.scrollOffset);
  contentRef.current = content;
  scrollOffsetRef.current = props.scrollOffset;

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    let cancelled = false;
    void import("@opentui/core").then(
      ({ ScrollBoxRenderable, TextRenderable }) => {
        if (cancelled) return;
        const scrollbox = new ScrollBoxRenderable(renderer, {
          id: "worker-event-log-scrollbox",
          width: "100%",
          height: "100%",
          scrollY: true,
          stickyScroll: true,
          stickyStart: "bottom",
          scrollbarOptions: {
            showArrows: true,
            trackOptions: {
              foregroundColor: theme.action.primary,
              backgroundColor: theme.surface.base,
            },
          },
        });
        const text = new TextRenderable(renderer, {
          id: "worker-event-log-text",
          width: "100%",
          height: "auto",
          content: contentRef.current,
          fg: theme.text.default,
          wrapMode: "word",
        });
        scrollbox.add(text);
        container.add(scrollbox);
        scrollRef.current = scrollbox;
        textRef.current = text;
        scrollbox.scrollTo({ x: 0, y: scrollOffsetRef.current });
      },
    );
    return () => {
      cancelled = true;
      if (scrollRef.current) {
        container.remove(scrollRef.current);
        scrollRef.current.destroy();
        scrollRef.current = null;
      }
      textRef.current = null;
    };
  }, [renderer, theme]);

  useEffect(() => {
    if (textRef.current) textRef.current.content = content;
    scrollRef.current?.scrollTo({ x: 0, y: props.scrollOffset });
  }, [content, props.scrollOffset]);

  return <box ref={containerRef} width="100%" flexGrow={1} marginBottom={1} />;
}
