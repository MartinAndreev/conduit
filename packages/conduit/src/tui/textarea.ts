export async function textarea({
  label,
  initialValue = "",
}: {
  label: string;
  initialValue?: string;
}): Promise<string> {
  const { createCliRenderer, TextareaRenderable, TextRenderable } =
    await import("@opentui/core");
  const renderer = await createCliRenderer({
    exitOnCtrlC: false,
    clearOnShutdown: true,
  });
  return new Promise<string>((resolve, reject) => {
    let settled = false;
    const finishResolve = (value: string) => {
      if (settled) return;
      settled = true;
      renderer.keyInput.off("keypress", onKeypress);
      renderer.destroy();
      resolve(value);
    };
    const finishReject = (error: Error) => {
      if (settled) return;
      settled = true;
      renderer.keyInput.off("keypress", onKeypress);
      renderer.destroy();
      reject(error);
    };
    const textareaEl = new TextareaRenderable(renderer, {
      id: "conduit-refine-textarea",
      width: "94%",
      height: 12,
      position: "absolute",
      top: 4,
      left: 2,
      initialValue,
      placeholder: label,
      placeholderColor: "#8B8B8B",
      backgroundColor: "#20251F",
      focusedBackgroundColor: "#2B332A",
      textColor: "#D8D5C8",
      focusedTextColor: "#E5E1D4",
      cursorColor: "#D8C28B",
      keyBindings: [{ name: "return", ctrl: true, action: "submit" }],
      onSubmit: () => finishResolve(textareaEl.plainText.trim()),
    });
    const title = new TextRenderable(renderer, {
      id: "conduit-refine-title",
      content: `${label}\nCtrl+Enter: save · Ctrl+C: cancel · arrows/editing work normally`,
      fg: "#8FB6A0",
      position: "absolute",
      top: 1,
      left: 2,
    });
    const onKeypress = (key: { ctrl?: boolean; name?: string }) => {
      if (key.ctrl && key.name === "c")
        finishReject(new Error("Refinement cancelled."));
    };
    renderer.root.add(title);
    renderer.root.add(textareaEl);
    renderer.keyInput.on("keypress", onKeypress);
    textareaEl.focus();
  });
}
