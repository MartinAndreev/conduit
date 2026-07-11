import eslint from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default [
  { ignores: ["dist/**", "node_modules/**", ".conduit/**"] },
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      globals: globals.node,
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
        ecmaFeatures: { jsx: true },
      },
    },
    rules: {
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_" },
      ],
      "@typescript-eslint/no-explicit-any": "warn",
    },
  },
  {
    files: ["**/*.{js,mjs,cjs}"],
    languageOptions: { globals: globals.node },
  },
  {
    files: ["scripts/build-standalone.js", "scripts/build.js"],
    languageOptions: { globals: { ...globals.node, Bun: "readonly" } },
  },
];
