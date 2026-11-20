// Package javascript is the code-model frontend for JavaScript and
// TypeScript, including JSX/TSX, CommonJS and TypeScript declaration files.
//
// We parse with a hand-written lexer and an error-tolerant recursive
// descent parser. Parse keeps no per-file state, so resolution re-parses
// each file to rebuild its scopes. It then resolves
// references lexically, through ESM imports, re-exports and CommonJS
// require/exports, tsconfig paths, and member access on this, typed
// variables, classes, namespaces and modules.
//
// Anonymous functions (callbacks, IIFEs) don't get a declaration. Their
// parameters and locals become local declarations of the enclosing named
// declaration, scoped to the anonymous function, so we can still find and
// rename them by position.
package javascript
