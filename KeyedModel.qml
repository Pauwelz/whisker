import QtQuick
import "state/Timeline.js" as Timeline

// A ListModel of row keys that only gets edits. A new model array scrolls a ListView back and rebuilds every delegate;
// edits leave the rows in view in place. A delegate has `required property string key` and reads its row from `rows`.
ListModel {
  id: keyed
  property var rows: ({}) // the row of each key

  // Shows `newRows` in order. `keyOf(row)` gives a row its key. `reset` clears the list first, so the view starts over.
  function show(newRows, keyOf, reset) {
    const keys = newRows.map(keyOf)
    const byKey = {}
    newRows.forEach(function (row, i) { byKey[keys[i]] = row })
    const before = []
    for (let i = 0; i < keyed.count; i++) before.push(keyed.get(i).key)
    const edits = reset ? null : Timeline.listEdits(before, keys)
    keyed.rows = Object.assign({}, keyed.rows, byKey) // removed and inserted rows both resolve during the edits
    if (edits) {
      edits.forEach(function (e) { if (e.key === undefined) keyed.remove(e.remove); else keyed.insert(e.insert, { key: e.key }) })
    } else {
      keyed.clear()
      keys.forEach(function (key) { keyed.append({ key: key }) })
    }
    keyed.rows = byKey
  }
}
