/** Adds a and b. */
export function add(a: number, b: number): number {
  return a + b;
}

export class Counter {
  private count = 0;

  increment(by: number): number {
    this.count += by;
    return this.count;
  }
}
