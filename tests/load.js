// Load a QML `.pragma library` JS file into a plain object, for node tests.
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

module.exports = function load(file) {
  const source = fs.readFileSync(path.join(__dirname, "..", file), "utf8").replace(/^\.pragma .*$/m, "");
  const context = vm.createContext({});
  vm.runInContext(source, context, { filename: file });
  return context;
};
