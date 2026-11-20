// SPDX-License-Identifier: MIT
"use strict";

const fs = require("node:fs");

/**
 * Looks the id up in the cache, loading it from disk on a miss.
 * @param {string} id cache key
 * @returns {object} the cached entry
 */
function load(id) {
  return JSON.parse(fs.readFileSync(id, "utf8"));
}

class Base {
  render() {
    throw new Error("not implemented");
  }
}

class Page extends Base {
  render() {
    return "page";
  }
}

function cleanup() {
  try {
    fs.unlinkSync("x");
  } catch {
    // the file may not exist yet on the first run
  }
}

function narrow() {
  try {
    fs.unlinkSync("y");
  } catch (e) {
    if (e.code !== "ENOENT") throw e;
  }
}

function show() {
  // eslint-disable-next-line no-console -- the CLI prints to stdout
  console.log("-".repeat(3));
  return fetch("/x").catch((e) => null);
}

function stripBullet(line) {
  return line.replace(/^(?:[-*•]|\d+[.)])\s+/, "");
}

function guarded(prev, other, rows, words, user) {
  const a = prev && other.style;
  const b = other.prev && prev.style;
  const c = prev && prev.size > 0;
  prev && prev.size = 1;
  const d = prev == null || other != null;
  const e = typeof prev == "object";
  const f = rows.filter((r) => r.ok)[1];
  const g = rows.filter((r) => r.ok).length > 1;
  const h = words.indexOf("x") === 0 || words.indexOf("y") > 0;
  const i = rows[other.length - 1];
  rows[rows.length - 1] = 0;
  const j = load()[load().length - 1];
  const k = Object.hasOwn(user, "id");
  const l = typeof window === "undefined";
  const m = user.name.replace(/-/, "_").replaceAll("-", "_");
  const n = Number.parseInt(user.size, 10) + user.isNaN(1);
  return [a, b, c, d, e, f, g, h, i, j, k, l, m, n];
}

class Range {
  isFinite(value) {
    return value < 10;
  }

  check(value) {
    return this.isFinite(value);
  }
}

function parseFloat(text) {
  return Number(text);
}

function lastArgument() {
  return arguments[arguments.length - 1];
}

function selected($items) {
  return $items.filter(".active")[0] || $items.filter(".open").length > 0;
}

function templates(name) {
  const tagged = html`<b>name</b>`;
  const raw = String.raw`C:\path`;
  const multi = `line one
line two`;
  const quotes = `it's "quoted"`;
  return [tagged, raw, multi, quotes, `hi ${name}`];
}

// The file was renamed from a.txt. This merges the old version of a paragraph
// with its new version, as requested by the client.
function rename(name) {
  if (!name) {
    throw new Error(`invalid name: ${name}`);
  }
  const error = name === "" ? "Invalid input" : null;
  return { error, message: "Saved " + name, method: "WOOHOO" };
}
