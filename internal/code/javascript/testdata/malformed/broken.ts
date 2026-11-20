import { a from './missing';

export class Broken {
  method( {
    const x = "unterminated
    return x;
  }

  other(): void {
    this.method(;
  }
}

function ok(value: number) {
  return value + 1;
}

const t = `open ${ ok(1)
let after = ok(2);
/* unterminated comment
