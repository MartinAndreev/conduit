import path from "node:path";
import { fileURLToPath } from "node:url";
import type { RolePortrait, PortraitRegistry } from "../types/portrait.js";

const currentDir = path.dirname(fileURLToPath(import.meta.url));
const assetsDir = path.resolve(currentDir, "../../../assets/portraits");

interface PortraitDef {
  roleName: string;
  label: string;
  assetFile: string;
  fallbackGlyph: string;
  art: readonly string[];
}

const PORTRAIT_ART: readonly PortraitDef[] = [
  {
    roleName: "architect",
    label: "Architect",
    assetFile: "architect.fb",
    fallbackGlyph: "\u2630",
    art: ["  /\\  ", " /  \\ ", "/____\\", " |  | ", " |__| "],
  },
  {
    roleName: "researcher",
    label: "Researcher",
    assetFile: "researcher.fb",
    fallbackGlyph: "\u2609",
    art: [" .--. ", "/  o \\", "|    |", "\\    /", " '--' "],
  },
  {
    roleName: "frontend",
    label: "Frontend",
    assetFile: "frontend.fb",
    fallbackGlyph: "\u25a3",
    art: [" ____ ", "| ## |", "| ## |", "|    |", "|____|"],
  },
  {
    roleName: "backend",
    label: "Backend",
    assetFile: "backend.fb",
    fallbackGlyph: "\u2699",
    art: ["  ()  ", " /||\\ ", "/ || \\", "  ||  ", " _||_ "],
  },
  {
    roleName: "qa",
    label: "QA",
    assetFile: "qa.fb",
    fallbackGlyph: "\u2714",
    art: ["      ", "    / ", "   /  ", "  /   ", " /    "],
  },
  {
    roleName: "documentation",
    label: "Docs",
    assetFile: "docs.fb",
    fallbackGlyph: "\u270e",
    art: [" ____ ", "|    |", "| || |", "| || |", "|____|"],
  },
  {
    roleName: "reviewer",
    label: "Reviewer",
    assetFile: "reviewer.fb",
    fallbackGlyph: "\u2605",
    art: ["  *   ", " ***  ", "*****", " ***  ", "  *   "],
  },
];

export function createPortraitRegistry(): PortraitRegistry {
  const portraits = new Map<string, PortraitDef>();
  for (const def of PORTRAIT_ART) {
    portraits.set(def.roleName, def);
  }
  const overrides = new Map<string, string>();

  return {
    getPortrait(roleName: string): RolePortrait {
      const def = portraits.get(roleName);
      if (!def) {
        return {
          roleName,
          label: roleName,
          assetPath: "",
          fallbackGlyph: "?",
        };
      }
      const overridePath = overrides.get(roleName);
      return {
        roleName: def.roleName,
        label: def.label,
        assetPath: overridePath ?? path.join(assetsDir, def.assetFile),
        fallbackGlyph: def.fallbackGlyph,
      };
    },

    getAllPortraits(): readonly RolePortrait[] {
      return PORTRAIT_ART.map((def) => {
        const overridePath = overrides.get(def.roleName);
        return {
          roleName: def.roleName,
          label: def.label,
          assetPath: overridePath ?? path.join(assetsDir, def.assetFile),
          fallbackGlyph: def.fallbackGlyph,
        };
      });
    },

    overrideAssetPath(roleName: string, assetPath: string): void {
      overrides.set(roleName, assetPath);
    },
  };
}
