//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestModalDialogGetsTheRightAdvice: while a modal dialog holds FreeCAD, a call that gives up
// waiting says a dialog is open and asks for it to be closed, as unavailable, not as a freecad_error
// about the call's arguments.
func TestModalDialogGetsTheRightAdvice(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EModal")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `from PySide import QtCore, QtWidgets
def _open():
    box = QtWidgets.QMessageBox()
    box.setWindowTitle('freecad-mcp test')
    box.setText('A modal dialog opened by a test; it closes itself.')
    QtCore.QTimer.singleShot(14000, box.accept)
    (getattr(box, 'exec', None) or box.exec_)()
QtCore.QTimer.singleShot(300, _open)
`}))
	time.Sleep(time.Second)

	// check_printability keeps its own short budget; list_objects waits 60 s, so a Go test covers its
	// reply (internal/mcpserver) and this one the live dispatch.
	r := call(t, cs, "check_printability", map[string]any{"doc_name": "E2EModal", "bed_x": 100, "bed_y": 100, "timeout": 3})
	fails(t, r, "code: unavailable", "a modal dialog is open in FreeCAD", "Ask the user to close it, then call again.")
	if strings.Contains(r.text, "Check the arguments and retry") {
		t.Fatalf("a dialog is not an argument problem:\n%s", r.text)
	}

	// The dialog closes itself; FreeCAD answers again.
	deadline := time.Now().Add(40 * time.Second)
	for {
		if res := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "print('back')"}); !res.isErr {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("FreeCAD stayed blocked after the dialog should have closed")
		}
		time.Sleep(2 * time.Second)
	}
}
