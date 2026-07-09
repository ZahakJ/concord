// Minimal lint gate. We are NOT adopting a style guide here — the one and only
// job is to catch "you referenced something that doesn't exist" (no-undef),
// the class of bug that let a stray `focused` template reference ship and crash
// the call UI at runtime while the build stayed green.
import svelte from "eslint-plugin-svelte";
import svelteParser from "svelte-eslint-parser";
import globals from "globals";

// Svelte 5 runes are compiler macros, not real globals — declare them so
// no-undef doesn't flag them.
const runes = {
  $state: "readonly",
  $derived: "readonly",
  $props: "readonly",
  $effect: "readonly",
  $bindable: "readonly",
  $inspect: "readonly",
  $host: "readonly",
};

export default [
  {
    files: ["src/**/*.js", "src/**/*.mjs"],
    languageOptions: {
      ecmaVersion: 2024,
      sourceType: "module",
      globals: { ...globals.browser, ...globals.node, ...runes },
    },
    rules: { "no-undef": "error" },
  },
  {
    files: ["src/**/*.svelte"],
    plugins: { svelte },
    languageOptions: {
      parser: svelteParser,
      ecmaVersion: 2024,
      sourceType: "module",
      globals: { ...globals.browser, ...runes },
    },
    rules: { "no-undef": "error" },
  },
];
