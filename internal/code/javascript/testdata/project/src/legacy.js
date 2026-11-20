import { Circle } from './lib';
import { Geometry as G } from '@lib/shapes';

export const unit = new Circle(2);

export default function legacy() {
  return unit.area() + G.distance(G.origin, G.origin);
}
