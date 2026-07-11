import path from "node:path";
import { fileURLToPath } from "node:url";
import { generatedRoleMascotFrames } from "../assets/generated-mascot-frames.js";
import type { PortraitRegistry } from "../interfaces/portrait-registry.js";
import type { RolePortrait } from "../interfaces/role-portrait.js";

const currentDir = path.dirname(fileURLToPath(import.meta.url));
const assetsDir = path.resolve(currentDir, "../../../../assets/mascots");

interface PortraitDef {
  roleName: string;
  label: string;
  fallbackGlyph: string;
}

const PORTRAITS: readonly PortraitDef[] = [
  { roleName: "architect", label: "Architect", fallbackGlyph: "☰" },
  { roleName: "researcher", label: "Researcher", fallbackGlyph: "☉" },
  { roleName: "frontend", label: "Frontend", fallbackGlyph: "▣" },
  { roleName: "backend", label: "Backend", fallbackGlyph: "⚙" },
  { roleName: "qa", label: "QA", fallbackGlyph: "✔" },
  { roleName: "documentation", label: "Docs", fallbackGlyph: "✎" },
  { roleName: "reviewer", label: "Reviewer", fallbackGlyph: "★" },
];

function toPortrait(def: PortraitDef, overridePath?: string): RolePortrait {
  return {
    roleName: def.roleName,
    label: def.label,
    assetPath: overridePath ?? path.join(assetsDir, def.roleName),
    fallbackGlyph: def.fallbackGlyph,
    frames: generatedRoleMascotFrames[def.roleName] ?? [["?"]],
  };
}

export function createPortraitRegistry(): PortraitRegistry {
  const portraits = new Map(PORTRAITS.map((def) => [def.roleName, def]));
  const overrides = new Map<string, string>();

  return {
    getPortrait(roleName: string): RolePortrait {
      const def = portraits.get(roleName);
      if (!def)
        return {
          roleName,
          label: roleName,
          assetPath: "",
          fallbackGlyph: "?",
          frames: [["?"]],
        };
      return toPortrait(def, overrides.get(roleName));
    },

    getAllPortraits(): readonly RolePortrait[] {
      return PORTRAITS.map((def) =>
        toPortrait(def, overrides.get(def.roleName)),
      );
    },

    overrideAssetPath(roleName: string, assetPath: string): void {
      overrides.set(roleName, assetPath);
    },
  };
}
