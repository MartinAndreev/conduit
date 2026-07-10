import eslint from "@eslint/js";
import globals from "globals";

export default [
  { ignores: ["dist/**", "node_modules/**", ".conduit/**"] },
  eslint.configs.recommended,
  {
    files: ["**/*.{js,mjs,cjs}"],
    languageOptions: { globals: globals.node },
  },
  {
    files: ["scripts/build-standalone.js", "scripts/build.js"],
    languageOptions: { globals: { ...globals.node, Bun: "readonly" } },
  },
];
