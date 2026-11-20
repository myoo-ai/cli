/**
 * positive.ts - typed helpers.
 * Everything here is reported.
 */
import { readFileSync } from "fs";

// ── Section ──

/** This is a function that reads the file. */
export function readFile(path: string): string {
  // Step 1: Read the bytes from the disk
  const text = readFileSync(path, "utf8");
  // Here we return the text to the caller
  return text;
}

export function steps(values: number[]): number {
  // 1. Sort the values in ascending order
  values.sort();
  // 2. Sum all of the values together
  return values.reduce((a, b) => a + b, 0);
}

export function total(prices: number[]): number {
  let sum = 0;
  // Add the price to the sum variable
  sum += prices[0];
  return sum;
}

export function placeholder(): void {
  throw new Error("TODO");
}

export function report(): void {
  console.log("📦 packed");
  console.log("• item");
  console.log("a — b");
  console.log("=".repeat(60));
}

export function swallow(): void {
  try {
    readFileSync("x");
  } catch (err) {
    // ignore
  }
}

// @ts-expect-error
const bad: number = "x";

export function chatty(x: number): number {
  // This explanation starts here
  // and keeps going
  // for a while
  // without saying much
  // about anything
  // in particular
  return x;
}

export function nested(a: number[][]): number {
  for (const row of a) {
    for (const cell of row) {
      if (cell > 0) {
        try {
          if (cell > 1) {
            return cell;
          }
        } finally {
          console.log(cell);
        }
      }
    }
  }
  return 0;
}

export function longFunction(): number {
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

export function copyOne(rows: Row[], limit: number): string[] {
  const result: string[] = [];
  for (const row of rows) {
    if (row.size > limit && row.name.startsWith("a")) {
      result.push(row.name + ":" + row.size * 2 + ":" + row.owner);
    } else if (row.size < 0) {
      throw new RangeError("negative size " + row.name);
    }
  }
  return result.slice(0, limit);
}

export function copyTwo(items: Item[], maximum: number): string[] {
  const found: string[] = [];
  for (const item of items) {
    if (item.weight > maximum && item.label.startsWith("b")) {
      found.push(item.label + ":" + item.weight * 3 + ":" + item.parent);
    } else if (item.weight < 0) {
      throw new RangeError("negative weight " + item.label);
    }
  }
  return found.slice(0, maximum);
}

export function guards(prev?: Row, list?: Item[], key = 0): unknown[] {
  const style = prev && prev.name;
  const first = list && list[key] && list[key].label.trim();
  return [style, first];
}

export async function modern(rows: any[], words: string[], user: Record<string, any>): Promise<unknown[]> {
  var count = 0;
  if (count == rows.length) {
    debugger;
  }
  const head = rows.filter((r) => r.ok).shift();
  const tail = rows.filter((r) => r.ok).at(0);
  const many = rows.filter((r) => r.ok).length !== 0;
  const missing = words.indexOf("x") < 0;
  const last = words[words.length - 2];
  const own = {}.hasOwnProperty.call(user, "id");
  const plain = typeof user.name !== "undefined";
  const text = words.join(" ").replace(/\s+/g, " ");
  const nan = isNaN(user.size as any);
  const child = await import("child_process");
  const label = user.label!.trim() + rows[0]!.id;
  return [head, tail, many, missing, last, own, plain, text, nan, child, label, count];
}

export const plainTemplate = `no substitutions`;

/** Same behavior as before. Renamed from fetchUserV1. */
export function loadUser(raw: unknown, cpu: object): User {
  // As requested, the cache is skipped. Feel free to tweak the timeout.
  // This is just a placeholder implementation for now.
  const queue = cpu as any as Record<string, Array<number>>;
  const user = raw as unknown as User;
  if (!user) throw new Error("Something went wrong");
  if (!queue) throw new TypeError("Invalid argument");
  return { ...user, bio: "Lorem ipsum dolor sit amet", status: "Oops! Something went wrong." };
}

export function selfCheck(): void {
  expect(1).toBe(1);
  expect("a").toEqual('a');
  expect(true).toBeTruthy();
  assert.ok(true);
  assert.strictEqual(-1, -1);
}
