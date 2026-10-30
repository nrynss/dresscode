/// <reference types="svelte" />
/// <reference types="vite/client" />

/** The gate's typecheck has no Node types package. These cover the fixture loader and its tests. */
declare module "node:fs" {
  interface ReadBytes {
    subarray(start: number, end: number): { toString(encoding: "hex"): string };
    readUInt32BE(offset: number): number;
  }
  interface Stats {
    isDirectory(): boolean;
  }
  export function readFileSync(path: string, encoding: "utf8"): string;
  export function readFileSync(path: string): ReadBytes;
  export function readdirSync(path: string): string[];
  export function statSync(path: string): Stats;
}

declare module "node:path" {
  export function dirname(path: string): string;
  export function resolve(...paths: string[]): string;
  export function join(...paths: string[]): string;
}

declare module "node:url" {
  export function fileURLToPath(url: string | URL): string;
}

interface DresscodeFixtureFile {
  file: string;
  json: unknown;
}

declare module "virtual:dresscode-api-fixtures" {
  const fixtures: DresscodeFixtureFile[];
  export default fixtures;
}
