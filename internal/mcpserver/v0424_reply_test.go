package mcpserver

import (
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/xmlrpc/xmlrpctest"
)

func TestListSubelementsSaysFacesAreNotFilteredByEdgeFilters(t *testing.T) {
	const line = "Faces are not filtered: curve, along, smooth and min_length apply to edges (surface and on_bottom apply to faces)."
	reply := func([]any) (any, error) {
		return map[string]any{"success": true, "face_count": int64(1), "edge_count": int64(1), "filters": map[string]any{},
			"faces": []any{map[string]any{"name": "Face1", "surface": "plane", "area": 4.0, "center": []any{0.0, 0.0, 0.0}}},
			"edges": []any{map[string]any{"name": "Edge1", "curve": "line", "length": 2.0, "start": []any{0.0, 0.0, 0.0}, "end": []any{2.0, 0.0, 0.0}}}}, nil
	}
	fc := addon(t, map[string]xmlrpctest.Handler{"list_subelements": reply})
	cs := session(t, settingsFor(fc))
	for _, c := range []struct {
		args map[string]any
		says bool
	}{
		{map[string]any{"kind": "all", "min_length": 1.0}, true},
		{map[string]any{"kind": "all", "along": "x", "curve": "line"}, true},
		{map[string]any{"kind": "all", "surface": "plane", "min_length": 1.0}, false},
		{map[string]any{"kind": "all", "on_bottom": true}, false},
		{map[string]any{"kind": "all"}, false},
		{map[string]any{"kind": "edges", "min_length": 1.0}, false},
	} {
		c.args["doc_name"], c.args["obj_name"] = "D", "Box"
		text := replyText(call(t, cs, "list_subelements", c.args))
		if got := strings.Contains(text, line); got != c.says {
			t.Errorf("%v: line present %v, want %v:\n%s", c.args, got, c.says, text)
		}
	}
}

func TestListDocumentsExplainsTheActiveViewMark(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{"get_documents": func([]any) (any, error) {
		return map[string]any{"success": true, "active_document": "D", "documents": []any{
			map[string]any{"name": "D", "label": "D", "file_name": "", "modified": false, "active": true,
				"views": []any{map[string]any{"type": "View3DInventor", "active": true}}}}}, nil
	}})
	text := replyText(call(t, session(t, settingsFor(fc)), "list_documents", map[string]any{}))
	if !strings.Contains(text, "* marks the active view.") {
		t.Errorf("reply lacks the footer line:\n%s", text)
	}
}

func TestPrintabilityMarginsSayWhenTheBuildHeightIsExceeded(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{"check_printability": func([]any) (any, error) {
		return map[string]any{"success": true, "printable": false, "overlaps": []any{}, "not_checked": []any{},
			"objects": []any{map[string]any{"name": "Tall", "label": "Tall", "size": []any{10.0, 10.0, 30.0}, "free_margin_mm": -5.0,
				"issues":    []any{"extends past z = 25 (the plate edge) by 5 mm"},
				"margin_mm": map[string]any{"x_low": 10.0, "x_high": 186.0, "y_low": 10.0, "y_high": 206.0, "z_low": 0.0, "z_high": -5.0}}}}, nil
	}})
	text := replyText(call(t, session(t, settingsFor(fc)), "check_printability", map[string]any{"doc_name": "D", "bed_x": 200, "bed_y": 200, "bed_z": 25}))
	if !strings.Contains(text, "y 10 / 206, exceeds the build height by 5 mm)") || strings.Contains(text, "-5 mm of build height left") {
		t.Errorf("reply = %s", text)
	}
}
