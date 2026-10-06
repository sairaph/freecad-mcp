package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/xmlrpc"
	"github.com/sairaph/freecad-mcp/internal/xmlrpc/xmlrpctest"
)

const gaveUp = "GUI dispatch gave up after 60.0s waiting for 'get_objects' to start (queue_timeout=60s) (GUI thread is not processing tasks: %s; 1 task(s) queued)"

// The addon words the guard that held the GUI thread (gui_dispatch.py); a dialog or an open menu is
// a person's to close, so the reply says so and is unavailable, not a freecad_error about arguments.
func TestDialogHoldingFreeCADIsUnavailableWithTheRightAdvice(t *testing.T) {
	for _, guard := range []string{"a modal dialog is open in FreeCAD", "a popup or context menu is open in FreeCAD"} {
		message := strings.Replace(gaveUp, "%s", guard, 1)
		fc := addon(t, map[string]xmlrpctest.Handler{
			// list_objects: the addon raises a fault.
			"get_objects": func([]any) (any, error) {
				return nil, &xmlrpc.Fault{Code: 1, String: "GUI_DISPATCH_FAILED: " + message}
			},
			// check_printability: the addon replies with a failure and its own timeout hint.
			"check_printability": func([]any) (any, error) {
				return map[string]any{"success": false, "code": "unavailable", "error": message,
					"hint": "Call check_printability again with a larger timeout (this call allowed 3 seconds)."}, nil
			},
		})
		cs := session(t, settingsFor(fc))
		for tool, args := range map[string]map[string]any{
			"list_objects":       {"doc_name": "D"},
			"check_printability": {"doc_name": "D", "bed_x": 100, "bed_y": 100, "timeout": 3},
		} {
			res := call(t, cs, tool, args)
			text := replyText(res)
			if !res.IsError || !strings.Contains(text, "code: unavailable") || !strings.Contains(text, "Ask the user to close it, then call again.") ||
				strings.Contains(text, "Check the arguments") || strings.Contains(text, "larger timeout") {
				t.Errorf("%s with %q: %s", tool, guard, text)
			}
		}
	}
}

// dialogBlocked matches the addon's own wording, so a reword on either side must break a test, not
// quietly bring back the argument hint.
func TestDialogPhrasesAreTheAddonsOwn(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "addon", "FreeCADMCP", "rpc_server", "gui_dispatch.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{"a modal dialog is open in FreeCAD", "a popup or context menu is open in FreeCAD"} {
		if !strings.Contains(string(data), phrase) {
			t.Errorf("gui_dispatch.py no longer says %q; update dialogBlocked to match", phrase)
		}
		if !dialogBlocked("(GUI thread is not processing tasks: " + phrase + "; 1 task(s) queued)") {
			t.Errorf("dialogBlocked does not match %q", phrase)
		}
	}
}

func TestOtherGuardsKeepTheirReply(t *testing.T) {
	message := strings.Replace(gaveUp, "%s", "mouse buttons held (3D navigation drag)", 1)
	fc := addon(t, map[string]xmlrpctest.Handler{
		"get_objects": func([]any) (any, error) {
			return nil, &xmlrpc.Fault{Code: 1, String: "GUI_DISPATCH_FAILED: " + message}
		},
	})
	cs := session(t, settingsFor(fc))
	res := call(t, cs, "list_objects", map[string]any{"doc_name": "D"})
	if text := replyText(res); !res.IsError || !strings.Contains(text, "code: freecad_error") || strings.Contains(text, "close it") {
		t.Errorf("reply = %s", text)
	}
}
