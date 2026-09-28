import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["takt-sandbox.mjs"],
    languageOptions: { globals: globals.node },
  },
  {
    files: ["takt-dag.tsx"],
    rules: {
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
    },
  },
  {
    files: ["takt-vfs.ts"],
    rules: {
      "@typescript-eslint/no-unused-vars": ["error", { varsIgnorePattern: "^CollisionError$" }],
    },
  },
);
