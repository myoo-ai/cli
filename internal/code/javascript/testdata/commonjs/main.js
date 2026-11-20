const math = require('./lib/math');
const { add, square: sq } = require('./lib/math.js');
const Counter = require('./lib/counter');
const path = require('path');

var counter = new Counter(0);
counter.increment().increment();

function main(argv) {
  var result = add(1, 2) + sq(3) + math.sum(4, 5);
  if (argv.length) {
    var result = math.multiply(result, 2);
  }
  return path.join(String(result), require('./lib/math').VERSION);
}

main(process.argv);

(function () {
  var hidden = 1;
  return hidden;
})();
