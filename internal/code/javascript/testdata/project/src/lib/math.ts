/** Adds two numbers. */
export function add(a: number, b: number): number {
  return a + b;
}

export const PI = 3.14159;

export function scale(value: number): number;
export function scale(value: string): string;
export function scale(value: any) {
  return value * PI;
}

export default class Calculator<T extends number = number> {
  private total: T;
  static instances = 0;

  constructor(private readonly precision: number, public label?: string) {
    this.total = 0 as T;
    Calculator.instances++;
  }

  add(x: T): this {
    this.total = add(this.total, x) as T;
    return this;
  }

  get value(): number {
    return round(this.total, this.precision);
  }
}

function round(n: number, digits = 2) {
  const f = 10 ** digits;
  return Math.round(n * f) / f;
}
