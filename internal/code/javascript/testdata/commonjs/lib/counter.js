const EventEmitter = require('events');

class Counter extends EventEmitter {
  constructor(start) {
    super();
    this.count = start;
    this.step = 1;
    this.increment = this.increment.bind(this);
  }

  increment() {
    this.count += this.step;
    this.emit('change', this.count);
    return this;
  }
}

module.exports = Counter;
