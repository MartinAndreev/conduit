import {
  createCliRenderer,
  TextareaRenderable,
  TextRenderable,
} from "@opentui/core";

export async function textarea({ label, initialValue = "" }) {
  const renderer = await createCliRenderer({
    exitOnCtrlC: false,
    clearOnShutdown: true,
  });
  return new Promise((resolve, reject) => {
    let settled = false;
    const finish = (callback, value) => {
      if (settled) return;
      settled = true;
      renderer.keyInput.off("keypress", onKeypress);
      renderer.destroy();
      callback(value);
    };
    const textarea = new TextareaRenderable(renderer, {
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
      onSubmit: () => finish(resolve, textarea.plainText.trim()),
    });
    const title = new TextRenderable(renderer, {
      id: "conduit-refine-title",
      content: `${label}\nCtrl+Enter: save · Ctrl+C: cancel · arrows/editing work normally`,
      fg: "#8FB6A0",
      position: "absolute",
      top: 1,
      left: 2,
    });
    const onKeypress = (key) => {
      if (key.ctrl && key.name === "c")
        finish(reject, new Error("Refinement cancelled."));
    };
    renderer.root.add(title);
    renderer.root.add(textarea);
    renderer.keyInput.on("keypress", onKeypress);
    textarea.focus();
  });
}
