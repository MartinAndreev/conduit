import { extractAppliedPatch } from "./dashboard.js";

interface FormatArchitectRunParams {
  featureId: string;
  transcript: string;
  completed?: boolean;
  transcriptVisible?: boolean;
}

export function formatArchitectRun({
  featureId,
  transcript,
  completed = false,
  transcriptVisible = false,
}: FormatArchitectRunParams): string {
  const lines = [
    "Conduit · Refining feature " + featureId,
    "",
    "› ○ architect",
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
      : "Status: Architect is working · dashboard opens when complete · q hides this view",
  );
  return lines.join("\n");
}

export async function startArchitectRunView({
  featureId,
  onUserClose = () => {},
}: {
  featureId: string;
  onUserClose?: () => void;
}) {
  const { createCliRenderer, TextRenderable } = await import("@opentui/core");
  const renderer = await createCliRenderer({
    exitOnCtrlC: false,
    clearOnShutdown: true,
  });
  let transcript = "";
  let completed = false;
  let closed = false;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let resolveClosed: any;
  const closedPromise = new Promise<{ user: boolean }>((resolve) => {
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
  const close = ({ user = false }: { user?: boolean } = {}) => {
    if (closed) return;
    closed = true;
    renderer.keyInput.off("keypress", onKeypress);
    renderer.destroy();
    if (user) onUserClose();
    resolveClosed({ user });
  };
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const onKeypress = (key: any) => {
    if (key.name === "q") return close({ user: true });
    if ((key.ctrl && key.name === "c") || (completed && key.name === "return"))
      close();
  };
  renderer.root.add(text);
  renderer.keyInput.on("keypress", onKeypress);
  return {
    update: (nextTranscript: string) => {
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
