import { createCliRenderer, TextRenderable } from "@opentui/core";
import { extractAppliedPatch } from "./dashboard.js";

export function formatArchitectRun({
  featureId,
  transcript,
  completed = false,
  transcriptVisible = false,
}) {
  const lines = [
    "Conduit · Refining feature " + featureId,
    "",
    "› ○ architect     codex",
  ];
  if (transcriptVisible) {
    lines.push("    ── captured transcript (tail) ──");
    lines.push(
      ...transcript
        .trim()
        .split("\n")
        .slice(-32)
        .map((line) => `    ${line}`),
    );
  } else {
    const commands = [...transcript.matchAll(/(?:^|\n)exec\n([^\n]+)/g)].map(
      (match) => match[1],
    );
    if (!commands.length)
      lines.push("    • Analyzing project context\n      └ working…");
    else
      commands
        .slice(-4)
        .forEach((command) =>
          lines.push(
            `    • Ran ${command}\n      └ transcript captured · Ctrl+T to view after completion`,
          ),
        );
    const patch = extractAppliedPatch(transcript);
    if (patch)
      lines.push(
        `    • Applied specification patch\n      └ ${(patch.match(/^diff --git /gm) ?? []).length} changed files · available in status --tui`,
      );
  }
  lines.push(
    "",
    completed
      ? "Status: refinement completed"
      : "Status: Codex is working · dashboard opens when complete · q hides this view",
  );
  return lines.join("\n");
}

export async function startArchitectRunView({
  featureId,
  onUserClose = () => {},
}) {
  const renderer = await createCliRenderer({
    exitOnCtrlC: false,
    clearOnShutdown: true,
  });
  let transcript = "";
  let completed = false;
  let closed = false;
  let resolveClosed;
  const closedPromise = new Promise((resolve) => {
    resolveClosed = resolve;
  });
  const text = new TextRenderable(renderer, {
    id: "conduit-architect-run",
    content: formatArchitectRun({
      featureId,
      transcript,
      completed,
      transcriptVisible: false,
    }),
    fg: "#D8D5C8",
    position: "absolute",
    left: 2,
    top: 1,
  });
  const refresh = () => {
    text.content = formatArchitectRun({
      featureId,
      transcript,
      completed,
      transcriptVisible: false,
    });
  };
  const close = ({ user = false } = {}) => {
    if (closed) return;
    closed = true;
    renderer.keyInput.off("keypress", onKeypress);
    renderer.destroy();
    if (user) onUserClose();
    resolveClosed({ user });
  };
  const onKeypress = (key) => {
    if (key.name === "q") return close({ user: true });
    if ((key.ctrl && key.name === "c") || (completed && key.name === "return"))
      close();
  };
  renderer.root.add(text);
  renderer.keyInput.on("keypress", onKeypress);
  return {
    update: (nextTranscript) => {
      transcript = nextTranscript;
      refresh();
    },
    complete: () => {
      completed = true;
      refresh();
    },
    waitForClose: () => closedPromise,
    close,
  };
}
