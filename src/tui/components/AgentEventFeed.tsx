import { useEffect, useRef } from "react";
import {
  BoxRenderable,
  DiffRenderable,
  RGBA,
  SyntaxStyle,
} from "@opentui/core";
import { useRenderer } from "@opentui/react";
import { useTheme } from "@tui/components/ThemeProvider.js";

const DEMO_PATCH = `diff --git a/package.json b/package.json
index 3a2b1c4..4f5e6d7 100644
--- a/package.json
+++ b/package.json
@@ -27,7 +27,7 @@
   "devDependencies": {
     "@eslint/js": "^9",
-    "eslint": "^9",
+    "eslint": "^9.1.0",
     "globals": "^17.0.0"
   }
 }`;

const DIFF_SYNTAX_STYLE = SyntaxStyle.fromStyles({
  default: { fg: RGBA.fromHex("#E6EDF3") },
  string: { fg: RGBA.fromHex("#A5D6FF") },
  keyword: { fg: RGBA.fromHex("#FF7B72"), bold: true },
});

/** Representative compact activity events until the runner supplies live data. */
export function AgentEventFeed() {
  const theme = useTheme();
  const renderer = useRenderer();
  const diffContainerRef = useRef<BoxRenderable>(null);

  useEffect(() => {
    const container = diffContainerRef.current;
    if (!container) return;
    const diff = new DiffRenderable(renderer, {
      id: "activity-demo-diff",
      diff: DEMO_PATCH,
      view: "split",
      syncScroll: true,
      width: "100%",
      height: 8,
      filetype: "json",
      syntaxStyle: DIFF_SYNTAX_STYLE,
      showLineNumbers: true,
      wrapMode: "none",
      addedBg: "#24452E",
      removedBg: "#4A2630",
      addedContentBg: "#24452E",
      removedContentBg: "#4A2630",
    });
    container.add(diff);
    return () => {
      container.remove(diff);
      diff.destroy();
    };
  }, [renderer]);

  return (
    <box flexDirection="column" marginTop={1} gap={1}>
      <box
        flexDirection="column"
        borderStyle="single"
        borderColor={theme.surface.raised}
        paddingLeft={1}
      >
        <box flexDirection="row">
          <text content="› Ran " fg={theme.text.muted} />
          <text content="pnpm lint" fg={theme.text.strong} />
        </box>
        <text
          content="  └ 6 packages · 42 lines captured · Enter to expand"
          fg={theme.text.muted}
        />
      </box>

      <box
        flexDirection="column"
        borderStyle="single"
        borderColor={theme.surface.raised}
        paddingLeft={1}
      >
        <text content="# Todos" fg={theme.text.strong} />
        <text content="☑ Stage relevant files" fg={theme.action.primary} />
        <text
          content="☑ Commit with a conventional message"
          fg={theme.action.primary}
        />
        <text content="☑ Push branch and create PR" fg={theme.action.primary} />
      </box>

      <box
        flexDirection="column"
        borderStyle="single"
        borderColor={theme.surface.raised}
        paddingLeft={1}
      >
        <text content="← Edited package.json" fg={theme.text.strong} />
        <box ref={diffContainerRef} height={8} width="100%" />
      </box>
    </box>
  );
}
