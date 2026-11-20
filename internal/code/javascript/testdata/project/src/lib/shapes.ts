import { PI } from './math.js';

export interface Shape {
  readonly name: string;
  area(): number;
}

export interface Named<T> extends Shape {
  rename(name: T): void;
}

export enum Color {
  Red = 1,
  Green = Red << 1,
  Blue,
}

function sealed(target: Function) {}

@sealed
export abstract class Base implements Shape {
  abstract readonly name: string;
  abstract area(): number;

  describe(): string {
    return `${this.name}: ${this.area().toFixed(2)}`;
  }
}

export class Circle extends Base {
  readonly name = 'circle';
  color: Color = Color.Red;
  #cache?: number;

  constructor(public radius: number) {
    super();
  }

  area(): number {
    this.#cache ??= PI * this.radius ** 2;
    return this.#cache;
  }

  static unit(): Circle {
    return new Circle(1);
  }

  describe(): string {
    return 'circle ' + super.describe();
  }
}

export namespace Geometry {
  export const origin: Point = { x: 0, y: 0 };
  export function distance(a: Point, b: Point): number {
    return Math.hypot(a.x - b.x, a.y - b.y);
  }
}

export type Point = { x: number; y: number };

export type Maybe<T> = T | null;

export function largest<S extends Shape>(shapes: S[]): S | undefined {
  let best: S | undefined;
  for (const s of shapes) {
    if (!best || s.area() > best.area()) best = s;
  }
  return best;
}
