// Load a QML `.pragma library` JS file into a plain object, for node tests.
// A `.import "X.js" as Y` line loads X.js (relative to the importing file) and injects it as Y.
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function loadFile(full, cache) {
  if (cache.has(full)) return cache.get(full);
  const context = vm.createContext({});
  cache.set(full, context);
  const source = fs.readFileSync(full, "utf8")
    .replace(/^\.pragma .*$/m, "")
    .replace(/^\.import "(.+)" as (\w+)$/gm, (_, rel, name) => {
      context[name] = loadFile(path.resolve(path.dirname(full), rel), cache);
      return "";
    });
  vm.runInContext(source, context, { filename: full });
  return context;
}

module.exports = function load(file) {
  return loadFile(path.join(__dirname, "..", file), new Map());
};
