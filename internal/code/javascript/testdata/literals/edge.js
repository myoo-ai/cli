const re = /[/\]]+\/(?<name>x)/giu;
const half = total / 2 / count;
const parts = `a${`nested ${inner}`}b${obj.key}c`;
const big = 1_000_000n + 0xFF_FFn + 0o17 + 0b1010 + .5e-3;
const division = (a + b) / c;
const tagged = html`<p>${title}</p>`;
let total = 10, count = 2, inner = 'i', title = 't', a = 1, b = 2, c = 3;
const obj = { key: 'k', 'quoted-key': 1, [count]: 2, re: /}/ };
if (/^x/.test(title)) total++;
<!-- html comment
--> also a comment
class Store {
  #items = [];
  has(x) {
    return #items in this && this.#items.includes(x);
  }
}
function html(strings, ...values) {
  return strings.raw.join('') + values.length;
}
