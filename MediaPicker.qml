import QtQuick.Dialogs

// The file picker of the composer. Panel.qml loads it with a Loader, because QtQuick.Dialogs is an optional Qt plugin.
// The Loader fails alone when the module is missing; the rest of the pop-out stays.
FileDialog {
  signal picked(url file)
  title: "Send an image or a document"
  fileMode: FileDialog.OpenFile
  onAccepted: picked(selectedFile)
  Component.onCompleted: open()
}
