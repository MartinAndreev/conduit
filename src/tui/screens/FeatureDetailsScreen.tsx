import { useEffect, useState } from "react";
import { useKeyboard } from "@opentui/react";
import type { QueryBus } from "@system/bus/query-bus.js";
import { useTheme } from "@tui/components/ThemeProvider.js";
import { MarkdownDocument } from "@tui/components/MarkdownDocument.js";

interface PacketContent {
  readonly spec: string;
  readonly story: string;
  readonly testCases: string;
  readonly plan: string;
  readonly tasks: string;
}

export function FeatureDetailsScreen({
  queryBus,
  featureId,
  onExit,
}: {
  queryBus: QueryBus;
  featureId: string;
  onExit: () => void;
}) {
  const theme = useTheme();
  const [content, setContent] = useState<PacketContent | null>(null);
  const [selectedFile, setSelectedFile] = useState(0);
  useEffect(() => {
    void queryBus
      .execute({ type: "getFeatureContent", featureId })
      .then((result) => {
        if (result.success) setContent(result.data as PacketContent);
      });
  }, [queryBus, featureId]);
  useKeyboard((event: { name: string }) => {
    if (event.name === "q" || event.name === "escape") onExit();
    if (event.name === "left")
      setSelectedFile((value) => Math.max(0, value - 1));
    if (event.name === "right")
      setSelectedFile((value) => Math.min(4, value + 1));
  });
  if (!content)
    return (
      <box width="100%" height="100%" backgroundColor={theme.surface.base}>
        <text content="Loading feature packet..." fg={theme.text.muted} />
      </box>
    );
  const files = [
    ["Spec", content.spec],
    ["Story", content.story],
    ["QA", content.testCases],
    ["Plan", content.plan],
    ["Tasks", content.tasks],
  ] as const;
  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.surface.base}
      padding={1}
    >
      <text
        content={`Feature ${featureId} · ←/→ switch files · q return home`}
        fg={theme.action.primary}
      />
      <box flexDirection="row" marginTop={1}>
        {files.map(([label], index) => (
          <text
            key={label}
            content={`${index === selectedFile ? "[" : " "}${label}${index === selectedFile ? "]" : " "} `}
            fg={
              index === selectedFile ? theme.action.primary : theme.text.muted
            }
          />
        ))}
      </box>
      <box
        flexGrow={1}
        marginTop={1}
        padding={1}
        backgroundColor={theme.surface.raised}
      >
        <MarkdownDocument
          content={files[selectedFile]?.[1] || "No content yet."}
        />
      </box>
    </box>
  );
}
