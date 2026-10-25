.pragma library

// Shared helpers of the state modules: the request line, the state copy, the Qt Enter key codes and a shared notice.

// The notice for a request that finds no connection. Several modules show it.
var NO_WHATSAPP = "Could not reach WhatsApp"

// One JSON-RPC request line for the Daemon. `params` is left out when undefined.
function request(id, method, params) {
  return JSON.stringify({ jsonrpc: "2.0", id: id, method: method, params: params }) + "\n"
}

// Qt key codes of the two Enter keys. The composer sends the code of the key that was pressed.
var KEY_RETURN = 0x01000004

var KEY_ENTER = 0x01000005

function withChanges(state, changes) {
  var next = {}
  for (var key in state) next[key] = state[key]
  for (var change in changes) next[change] = changes[change]
  return next
}
