const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const load = require("./load");

test("load resolves .import lines relative to the importing file, with one instance per file", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "load-"));
  fs.mkdirSync(path.join(dir, "sub"));
  fs.writeFileSync(path.join(dir, "sub", "Base.js"), ".pragma library\nvar counter = { n: 0 }\nfunction bump() { return ++counter.n }\n");
  fs.writeFileSync(path.join(dir, "sub", "Left.js"), ".pragma library\n.import \"Base.js\" as Base\nfunction go() { return Base.bump() }\n");
  fs.writeFileSync(path.join(dir, "Top.js"), ".pragma library\n.import \"sub/Left.js\" as Left\n.import \"sub/Base.js\" as Base\nfunction go() { return Left.go() + Base.bump() }\n");
  const top = load(path.relative(path.join(__dirname, ".."), path.join(dir, "Top.js")));
  assert.equal(top.go(), 3); // Left and Top share one Base: 1 + 2
});
