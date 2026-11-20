#!/usr/bin/env node
import Calculator, { add as plus, PI } from './lib/math.js';
import * as lib from '@lib/index';
import { Circle, Color, Geometry, type Shape } from '@shapes';
import { readFileSync } from 'node:fs';

const circle: Circle = Circle.unit();
const calc = new Calculator(2, 'main');

export function run(shapes: Shape[]): number {
  let total = plus(PI, lib.sum(1, 2));
  for (const shape of shapes) {
    total += shape.area();
  }
  calc.add(total).add(circle.radius);
  return lib.largest(shapes)?.area() ?? total;
}

export function shadowing(PI: number) {
  {
    const PI = 3;
    hoisted = PI;
  }
  var hoisted: number;
  const inner = () => PI + hoisted;
  return inner();
}

class Report {
  private calc = new lib.Calc(1);
  constructor(private source: Shape) {}

  render(): string {
    const c = Color.Green;
    const d = Geometry.distance(Geometry.origin, { x: 1, y: 1 });
    return this.source.name + this.calc.value + c + d;
  }
}

new Report(circle).render();
readFileSync('x');
lib.math.add(1, 2);
