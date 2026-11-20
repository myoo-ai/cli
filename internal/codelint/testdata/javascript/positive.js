/**
 * positive.js - helpers for the demo.
 * Provides lookups and reports.
 */
"use strict";

// ==========================================

/** This is a function that gets the user. */
function getUser(id) {
  // Step 1: Look the user up in the store
  const user = lookup(id);
  // Let's return the user to the caller
  return user;
}

function count(items) {
  let counter = items.length;
  // Increment the counter variable here
  counter++;
  return counter;
}

function steps(text) {
  // First, split the text into its parts
  const parts = text.split(" ");
  // Then, sort the parts in natural order
  parts.sort();
  // Finally, join the parts with commas
  return parts.join(",");
}

function placeholder() {
  throw new Error("Not implemented");
}

function todo() {
  // TODO
}

function report() {
  console.log("✨ ready");
  console.log("a → b");
  console.log("x — y");
  console.log("-".repeat(40));
  console.log("==========");
}

function swallow() {
  try {
    risky();
  } catch (e) {}
  try {
    risky();
  } catch (e) {
    console.error(e);
  }
}

// @ts-ignore
const loose = window.thing;
// eslint-disable-next-line
const looser = window.other;

function chatty(x) {
  // This explanation starts here
  // and keeps going
  // for a while
  // without saying much
  // about anything
  // in particular
  return x;
}

function nested(a) {
  if (a) {
    for (const b of a) {
      while (b.next) {
        switch (b.kind) {
          case 1:
            if (b.ok) {
              return b;
            }
        }
      }
    }
  }
  return null;
}

function longFunction() {
  let total = 0;
  total += 0;
  total += 1;
  total += 2;
  total += 3;
  total += 4;
  total += 5;
  total += 6;
  total += 7;
  total += 8;
  total += 9;
  total += 10;
  total += 11;
  total += 12;
  total += 13;
  total += 14;
  total += 15;
  total += 16;
  total += 17;
  total += 18;
  total += 19;
  total += 20;
  total += 21;
  total += 22;
  total += 23;
  total += 24;
  total += 25;
  total += 26;
  total += 27;
  total += 28;
  total += 29;
  total += 30;
  total += 31;
  total += 32;
  total += 33;
  total += 34;
  total += 35;
  total += 36;
  total += 37;
  total += 38;
  total += 39;
  total += 40;
  total += 41;
  total += 42;
  total += 43;
  total += 44;
  total += 45;
  total += 46;
  total += 47;
  total += 48;
  total += 49;
  total += 50;
  total += 51;
  total += 52;
  total += 53;
  total += 54;
  total += 55;
  total += 56;
  total += 57;
  total += 58;
  total += 59;
  total += 60;
  total += 61;
  total += 62;
  total += 63;
  total += 64;
  total += 65;
  total += 66;
  total += 67;
  total += 68;
  total += 69;
  total += 70;
  total += 71;
  total += 72;
  total += 73;
  total += 74;
  total += 75;
  total += 76;
  total += 77;
  total += 78;
  total += 79;
  total += 80;
  total += 81;
  return total;
}

function copyOne(rows, limit) {
  const result = [];
  for (const row of rows) {
    if (row.size > limit && row.name.startsWith("a")) {
      result.push([row.name, row.size * 2, row.owner]);
    } else if (row.size < 0) {
      throw new RangeError("negative size " + row.name);
    }
  }
  return result.slice(0, limit);
}

function copyTwo(items, maximum) {
  const found = [];
  for (const item of items) {
    if (item.weight > maximum && item.label.startsWith("b")) {
      found.push([item.label, item.weight * 3, item.parent]);
    } else if (item.weight < 0) {
      throw new RangeError("negative weight " + item.label);
    }
  }
  return found.slice(0, maximum);
}

function guards(prev, list, map, key) {
  const style = prev && prev.style;
  const name = list && list.first && list.first.name();
  const item = map && map[key];
  return [style, name, item];
}

async function modern(rows, words, user) {
  var count = 0;
  if (count == rows.length) {
    debugger;
  }
  const first = rows.filter((r) => r.ok)[0];
  const many = rows.filter((r) => r.ok).length > 0;
  const none = rows.filter((r) => r.bad).length === 0;
  const found = words.indexOf("x") !== -1;
  const last = rows[rows.length - 1];
  const own = Object.prototype.hasOwnProperty.call(user, "id");
  const plain = typeof user.name === "undefined";
  const local = "undefined" !== typeof count;
  const text = user.name.replace(/-/g, "_");
  const size = parseInt(user.size, 10);
  const child = require("child_process");
  const fsp = await import("fs/promises");
  return [first, many, none, found, last, own, plain, local, text, size, child, fsp];
}

function lastHit(list) {
  for (var i = 0; i < list.length; i++) {
    if (list[i]) break;
  }
  return i;
}

function count(list) {
  var n = 0;
  if (list) {
    var n = list.length;
  }
  return n;
}

function escapeTags(html) {
  return html.replace(/[<>]/g, function (c) {
    return c === "<" ? "&lt;" : "&gt;";
  });
}

const plainTemplate = `no substitutions`;

// The old implementation used a loop. NEW: callers pass a list.
function save(items) {
  if (!items) {
    return Promise.reject(Error("An error occurred"));
  }
  assert(true);
  return "Congrats, the items were saved successfully!";
}
