import { add, Counter } from "./math";

const counter = new Counter();
console.log(add(1, 2), counter.increment(add(3, 4)));
