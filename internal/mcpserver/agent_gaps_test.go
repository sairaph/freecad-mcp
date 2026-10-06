package mcpserver

import (
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/xmlrpc/xmlrpctest"
)

func TestMarginZeroSaysItTouchesTheEdgeAndIsInside(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"check_printability": func([]any) (any, error) {
			return map[string]any{"success": true, "printable": true, "overlaps": []any{}, "not_checked": []any{},
				"objects": []any{
					map[string]any{"name": "A", "label": "A", "issues": []any{}, "size": []any{10.0, 10.0, 5.0}, "free_margin_mm": 0.0},
					map[string]any{"name": "B", "label": "B", "issues": []any{}, "size": []any{10.0, 10.0, 5.0}, "free_margin_mm": 12.5},
				}}, nil
		},
	})
	text := replyText(call(t, session(t, settingsFor(fc)), "check_printability", map[string]any{"doc_name": "D", "bed_x": 100, "bed_y": 100}))
	if !strings.Contains(text, "free margin to the plate edges 0 mm (touches the plate edge, still inside)") ||
		!strings.Contains(text, "free margin to the plate edges 12.5 mm.") {
		t.Errorf("reply = %s", text)
	}
}

func TestExportNamesTheLabelsTheFileHolds(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"export_document": func([]any) (any, error) {
			return map[string]any{"success": true, "document": "D", "file_name": "/x.3mf", "format": "3mf", "exporter": "Mesh.export",
				"bytes": 10, "objects": []any{"Lid", "Base_Plate"}, "skipped": []any{}, "units": "mm", "warnings": []any{},
				"labels": map[string]any{"Base_Plate": "Base Plate"}}, nil
		},
	})
	text := replyText(call(t, session(t, settingsFor(fc)), "export_document", map[string]any{"doc_name": "D", "path": "/x.3mf"}))
	if !strings.Contains(text, `Exported: Lid, "Base Plate" (object Base_Plate).`) {
		t.Errorf("reply = %s", text)
	}
}

func TestCreateAndUpdateEchoLabelAndPlacement(t *testing.T) {
	reply := func([]any) (any, error) {
		return map[string]any{"success": true, "object_name": "Base_Plate", "label": "Base Plate", "placement": "Base (5, 6, 0)"}, nil
	}
	fc := addon(t, map[string]xmlrpctest.Handler{"create_object": reply, "edit_object": reply})
	cs := session(t, settingsFor(fc))
	for tool, args := range map[string]map[string]any{
		"create_object": {"doc_name": "D", "obj_name": "Base Plate", "obj_type": "Part::Box", "include_screenshot": false},
		"update_object": {"doc_name": "D", "obj_name": "Base_Plate", "obj_properties": map[string]any{"Label": "Base Plate"}, "include_screenshot": false},
	} {
		text := replyText(call(t, cs, tool, args))
		if !strings.Contains(text, `Label: "Base Plate"; the name other tools take is "Base_Plate".`) || !strings.Contains(text, "Placement now: Base (5, 6, 0).") {
			t.Errorf("%s reply = %s", tool, text)
		}
	}
}

func TestListSubelementsShowsEdgeEndsAndAxis(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"list_subelements": func([]any) (any, error) {
			return map[string]any{"success": true, "edges": []any{
				map[string]any{"name": "Edge1", "curve": "line", "length": 10.0, "start": []any{0.0, 0.0, 0.0}, "end": []any{0.0, 0.0, 10.0}, "direction": []any{0.0, 0.0, 1.0}, "along": "z"},
				map[string]any{"name": "Edge2", "curve": "line", "length": 14.1421, "start": []any{0.0, 0.0, 0.0}, "end": []any{10.0, 10.0, 0.0}, "direction": []any{0.7071, 0.7071, 0.0}},
			}}, nil
		},
	})
	text := replyText(call(t, session(t, settingsFor(fc)), "list_subelements", map[string]any{"doc_name": "D", "obj_name": "Box", "kind": "edges"}))
	for _, want := range []string{"from (0, 0, 0) to (0, 0, 10), along z", "from (0, 0, 0) to (10, 10, 0), direction (0.7071, 0.7071, 0)"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q:\n%s", want, text)
		}
	}
}

func TestExecuteCodeErrorShowsTheTracebackAndNoReportViewAdvice(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"execute_code": func([]any) (any, error) {
			return map[string]any{"success": false, "error": "ValueError: boom",
				"traceback": "Traceback (most recent call last):\n  File \"<string>\", line 2, in <module>\nValueError: boom"}, nil
		},
	})
	res := call(t, session(t, settingsFor(fc)), "execute_code", map[string]any{"code": "x", "include_screenshot": false})
	text := replyText(res)
	if !res.IsError || !strings.Contains(text, "The traceback is below.") || !strings.Contains(text, `File "<string>", line 2, in <module>`) ||
		strings.Contains(text, "Report View") {
		t.Errorf("reply = %s", text)
	}
}
