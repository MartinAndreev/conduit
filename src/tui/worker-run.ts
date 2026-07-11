interface FormatWorkerRunParams {
  featureId: string;
  roles?: string[];
  events: string[];
  status: string;
}

export function formatWorkerRun({
  featureId,
  events,
  status,
}: FormatWorkerRunParams): string {
  const lines = [
    `Conduit · Implementing feature ${featureId}`,
    "",
    "› ○ worker team",
  ];
  for (const event of events.slice(-8)) {
    const [summary, ...details] = event.split("\n");
    lines.push(`    • ${summary}`);
    lines.push(...details.map((line) => `      ${line}`));
  }
  lines.push(
    "",
    `Status: ${status} · dashboard opens when complete · q hides this view · Ctrl+C cancels`,
  );
  return lines.join("\n");
}

export async function startWorkerRunView({
  featureId,
  roles,
  onCancel = () => {},
  onUserClose = () => {},
}: {
  featureId: string;
  roles: string[];
  onCancel?: () => void;
  onUserClose?: () => void;
}) {
  const { createCliRenderer, TextRenderable } = await import("@opentui/core");
  const renderer = await createCliRenderer({
    exitOnCtrlC: false,
    clearOnShutdown: true,
  });
  const events = roles.map((role) => `${role}: queued`);
  let status = "workers are preparing";
  const text = new TextRenderable(renderer, {
    id: "conduit-worker-run",
    content: formatWorkerRun({ featureId, events, status }),
    fg: "#D8D5C8",
    position: "absolute",
    left: 2,
    top: 1,
  });
  const refresh = () => {
    text.content = formatWorkerRun({ featureId, events, status });
  };
  const close = ({ user = false }: { user?: boolean } = {}) => {
    renderer.keyInput.off("keypress", onKeypress);
    renderer.destroy();
    if (user) onUserClose();
  };
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const onKeypress = (key: any) => {
    if (key.ctrl && key.name === "c") {
      onCancel();
      close({ user: true });
    } else if (key.name === "q") {
      close({ user: true });
    }
  };
  renderer.root.add(text);
  renderer.keyInput.on("keypress", onKeypress);
  return {
    updateStatus: (nextStatus: string) => {
      status = nextStatus;
      refresh();
    },
    appendEvent: (event: string) => {
      events.push(event);
      refresh();
    },
    close,
  };
}
