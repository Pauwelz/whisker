import QtQuick
import "WhiskerState.js" as State

// Put it inside a ListView or Flickable and pass that list as `view`: the wheel then scrolls a fixed step per notch
// (see State.wheelScrollTarget), in place of Qt's slow wheel flick. A touchpad scrolls at the same rate.
// `view` must be explicit: a Flickable moves its children onto its contentItem, so `parent` is not the list.
WheelHandler {
  required property Flickable view
  acceptedDevices: PointerDevice.Mouse | PointerDevice.TouchPad
  onWheel: function(event) {
    view.contentY = State.wheelScrollTarget(view.contentY, event.angleDelta.y,
                                            view.originY, view.originY + view.contentHeight - view.height)
  }
}
