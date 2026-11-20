"use client";
import { readFileSync } from "node:fs";

export abstract class Shape {
  abstract area(): number;
  describe(): string {
    throw new Error("not implemented");
  }
}

/** Parses the config file at path. Missing keys fall back to defaults. */
export function parseConfig(path: string): Record<string, string> {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch (err) {
    // a missing or broken config file means the defaults apply
    return {};
  }
}

// @ts-expect-error: the vendored typings miss this overload
const legacy: number = Number.parseInt("1", 10, 1);

export function arrows(): string {
  return "a -> b";
}

declare const __DEV__: boolean;

declare global {
  var __APP__: string;
}

export class Holder {
  value!: string;
  any = "any";
  isNaN(value: number): boolean {
    return value !== value;
  }
}

export function strict(holder: Holder, flag: boolean, items: unknown[]): unknown[] {
  let late!: number;
  const dev = typeof __DEV__ !== "undefined";
  const off = !flag && holder.value !== "x";
  const keys = { any: 1 }.any;
  const nan = holder.isNaN(late);
  return [dev, off, keys, nan, items.at(-1)];
}

export const styled = css`color: red`;

/** The figure meta of a markdown-it token, which the plugin API types as unknown. */
export function figure(token: { meta: unknown }, el: Element): object {
  // markdown-it types env as any, the figure plugin stores FigureMeta there
  const meta = token.meta as unknown as { width: number };
  const styles = getComputedStyle(el) as unknown as Record<string, string>; // CSSStyleDeclaration lacks an index signature
  return { meta, styles, placeholder: "John Doe", hint: `oops: width ${meta.width}` };
}

export function checks(value: number): void {
  expect(value).toBe(1);
  expect(1).not.toBe(2);
  assert.equal(value, 1);
}
