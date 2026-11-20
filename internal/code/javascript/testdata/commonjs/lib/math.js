'use strict';

function add(a, b) {
  return a + b;
}

exports.multiply = function (a, b) {
  return a * b;
};

module.exports.VERSION = '1.0';

module.exports = {
  add,
  sum: add,
  square(x) {
    return exports.multiply(x, x);
  },
};
