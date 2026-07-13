import { useEffect, useState } from "react";
import { useRenderer } from "@opentui/react";
import type { TerminalCapabilities } from "@opentui/core";
import { getSubmitKeyLabel } from "@tui/helpers/submit-key.js";

export function useTerminalSubmitKey() {
  const renderer = useRenderer();
  const [kittyKeyboardSupported, setKittyKeyboardSupported] = useState(
    renderer.capabilities?.kitty_keyboard === true,
  );

  useEffect(() => {
    const updateCapabilities = (capabilities: TerminalCapabilities) => {
      setKittyKeyboardSupported(capabilities.kitty_keyboard);
    };

    renderer.on("capabilities", updateCapabilities);
    setKittyKeyboardSupported(renderer.capabilities?.kitty_keyboard === true);
    return () => {
      renderer.off("capabilities", updateCapabilities);
    };
  }, [renderer]);

  return {
    kittyKeyboardSupported,
    label: getSubmitKeyLabel(kittyKeyboardSupported),
  };
}
