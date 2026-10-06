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

func TestCreateAndUpdateListFilletEdgesAndHiddenSource(t *testing.T) {
	reply := func([]any) (any, error) {
		return map[string]any{"success": true, "object_name": "Round", "edges": []any{"Edge1 r4", "Edge3 r2 to r4"}, "hidden": []any{"Outer"}}, nil
	}
	fc := addon(t, map[string]xmlrpctest.Handler{"create_object": reply, "edit_object": reply})
	cs := session(t, settingsFor(fc))
	for tool, args := range map[string]map[string]any{
		"create_object": {"doc_name": "D", "obj_name": "Round", "obj_type": "Part::Fillet", "include_screenshot": false},
		"update_object": {"doc_name": "D", "obj_name": "Round", "obj_properties": map[string]any{"Radius": 4}, "include_screenshot": false},
	} {
		text := replyText(call(t, cs, tool, args))
		if !strings.Contains(text, "Edges now: Edge1 r4, Edge3 r2 to r4.") ||
			!strings.Contains(text, "Hidden: Outer (the source of Round, as FreeCAD's own command does).") {
			t.Errorf("%s reply = %s", tool, text)
		}
	}
}

func TestExecuteCodeErrorShowsTheOutputPrintedBeforeIt(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"execute_code": func([]any) (any, error) {
			return map[string]any{"success": false, "error": "ValueError: boom", "output": "one\ntwo\n",
				"traceback": "Traceback (most recent call last):\nValueError: boom"}, nil
		},
	})
	res := call(t, session(t, settingsFor(fc)), "execute_code", map[string]any{"code": "x", "include_screenshot": false})
	text := replyText(res)
	if !res.IsError || !strings.Contains(text, "Output before the error:") || !strings.Contains(text, "one\ntwo") ||
		strings.Index(text, "Output before the error:") > strings.Index(text, "Traceback:") {
		t.Errorf("reply = %s", text)
	}
}

func TestPrintabilityShowsTheMarginToEachSide(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"check_printability": func([]any) (any, error) {
			return map[string]any{"success": true, "printable": true, "overlaps": []any{}, "not_checked": []any{},
				"objects": []any{map[string]any{"name": "A", "label": "A", "issues": []any{}, "size": []any{10.0, 10.0, 5.0},
					"free_margin_mm": 10.0, "margin_mm": map[string]any{"x_low": 10.0, "x_high": 186.0, "y_low": 10.0, "y_high": 206.0, "z_low": 0.0, "z_high": 24.5}}}}, nil
		},
	})
	text := replyText(call(t, session(t, settingsFor(fc)), "check_printability", map[string]any{"doc_name": "D", "bed_x": 220, "bed_y": 220, "bed_z": 30}))
	if !strings.Contains(text, "free margin to the plate edges 10 mm (x 10 / 186, y 10 / 206, z max 24.5)") {
		t.Errorf("reply = %s", text)
	}
}

func TestListSubelementsMarksDegenerateEdgesAndTheBottom(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"list_subelements": func([]any) (any, error) {
			return map[string]any{"success": true,
				"faces": []any{map[string]any{"name": "Face1", "surface": "plane", "area": 4.0, "center": []any{0.0, 0.0, 0.0}, "on_bottom": true}},
				"edges": []any{
					map[string]any{"name": "Edge28", "curve": "other", "length": 0.0, "start": []any{1.0, 1.0, 1.0}, "end": []any{1.0, 1.0, 1.0}, "degenerate": true},
					map[string]any{"name": "Edge2", "curve": "line", "length": 5.0, "start": []any{0.0, 0.0, 0.0}, "end": []any{5.0, 0.0, 0.0}, "along": "x", "on_bottom": true},
				}}, nil
		},
	})
	text := replyText(call(t, session(t, settingsFor(fc)), "list_subelements", map[string]any{"doc_name": "D", "obj_name": "Box", "kind": "all"}))
	for _, want := range []string{"degenerate (no length, a pole of a rounded corner): never pick it", "along x, on the bottom", "center (0, 0, 0), on the bottom"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q:\n%s", want, text)
		}
	}
}

func TestSetViewSaysWhichModeItStopped(t *testing.T) {
	for stopped, want := range map[string]string{
		"replaced": "Stopped the running orbit.",
		"user":     "The orbit had stopped: the user moved the view.",
		"finished": "The orbit had finished.",
	} {
		fc := addon(t, map[string]xmlrpctest.Handler{
			"set_view": func([]any) (any, error) {
				return map[string]any{"success": true, "document": "D", "mode": "static", "running": false, "stopped": stopped, "stopped_mode": "orbit"}, nil
			},
		})
		text := replyText(call(t, session(t, settingsFor(fc)), "set_view", map[string]any{"include_screenshot": false}))
		if !strings.Contains(text, want) {
			t.Errorf("stopped %s: reply = %s", stopped, text)
		}
	}
}

func TestSetViewFailureStillSaysWhichModeItStopped(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"set_view": func([]any) (any, error) {
			return map[string]any{"success": false, "code": "invalid_input", "error": "None of the focus objects has a shape on screen to frame.",
				"stopped": "replaced", "stopped_mode": "orbit"}, nil
		},
	})
	res := call(t, session(t, settingsFor(fc)), "set_view", map[string]any{"include_screenshot": false})
	if text := replyText(res); !res.IsError || !strings.Contains(text, "Stopped the running orbit.") {
		t.Errorf("reply = %s", text)
	}
}

func TestStoppedModeTextForAnOtherReasonHasNoReasonInIt(t *testing.T) {
	if got := stoppedModeText("stopped", "orbit"); got != "The orbit had stopped." {
		t.Errorf("text = %q", got)
	}
}
