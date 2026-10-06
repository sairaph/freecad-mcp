package mcpserver

import (
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/xmlrpc/xmlrpctest"
)

func TestCompactListLeavesOutOriginMembersAndSaysSoOnce(t *testing.T) {
	row := func(name, typ string, member bool) map[string]any {
		return map[string]any{"name": name, "label": name, "type": typ, "state": []any{}, "valid": true, "parent": "PartA",
			"solids": nil, "visible": true, "origin_member": member}
	}
	list := []any{row("PartA", "App::Part", false), row("Origin", "App::Origin", false),
		row("X_Axis", "App::Line", true), row("XY_Plane", "App::Plane", true), row("Leaf", "Part::Box", false)}
	fc := addon(t, map[string]xmlrpctest.Handler{"get_objects": func([]any) (any, error) { return list, nil }})
	cs := session(t, settingsFor(fc))

	text := replyText(call(t, cs, "list_objects", map[string]any{"doc_name": "D", "compact": true}))
	for _, want := range []string{"| PartA |", "| Origin |", "| Leaf |", "count: 3",
		"Origin axes and planes are left out: 7 per Origin; list_objects with compact false lists them."} {
		if !strings.Contains(text, want) {
			t.Errorf("compact list lacks %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"X_Axis", "XY_Plane"} {
		if strings.Contains(text, gone) {
			t.Errorf("compact list has %s:\n%s", gone, text)
		}
	}

	// A document with no Origin member says nothing, and the full list is untouched.
	plain := []any{map[string]any{"name": "Box", "label": "Box", "type": "Part::Box", "state": []any{}, "valid": true, "origin_member": false}}
	fc = addon(t, map[string]xmlrpctest.Handler{"get_objects": func([]any) (any, error) { return plain, nil }})
	text = replyText(call(t, session(t, settingsFor(fc)), "list_objects", map[string]any{"doc_name": "D", "compact": true}))
	if strings.Contains(text, "left out") {
		t.Errorf("a list with nothing left out says so:\n%s", text)
	}
	fc = addon(t, map[string]xmlrpctest.Handler{"get_objects": func([]any) (any, error) { return list, nil }})
	text = replyText(call(t, session(t, settingsFor(fc)), "list_objects", map[string]any{"doc_name": "D"}))
	if !strings.Contains(text, "X_Axis") || strings.Contains(text, "Origin axes") || !strings.Contains(text, "Volume, area and centre of mass are left out of this list: get_object gives them for one object.") || !strings.Contains(text, "count: 5") {
		t.Errorf("the full list changed:\n%s", text)
	}
}

func TestCheckPrintabilityGivesTheZRangeAndTheFloatingWarning(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{"check_printability": func([]any) (any, error) {
		return map[string]any{"success": true, "printable": true, "overlaps": []any{}, "not_checked": []any{},
			"objects": []any{
				map[string]any{"name": "PartA", "label": "PartA", "size": []any{40.0, 30.0, 3.0}, "free_margin_mm": 20.0,
					"bound_box": []any{20.0, 20.0, 5.0, 60.0, 50.0, 8.0},
					"issues":    []any{}, "warnings": []any{"floats 5 mm above the plate: it needs slicer supports, or move it down so its lowest point is at z 0"}},
				map[string]any{"name": "Flat", "label": "Flat", "size": []any{10.0, 10.0, 3.0}, "free_margin_mm": 12.5,
					"bound_box": []any{100.0, 100.0, -0.0, 110.0, 110.0, 3.0000000001}, "issues": []any{}},
			}}, nil
	}})
	text := replyText(call(t, session(t, settingsFor(fc)), "check_printability", map[string]any{"doc_name": "D", "bed_x": 220, "bed_y": 220}))
	for _, want := range []string{"- PartA: inside the plate, no overlap", "Warning: floats 5 mm above the plate: it needs slicer supports, or move it down so its lowest point is at z 0.",
		"size 40 x 30 x 3 mm; z 5 to 8; free margin", "size 10 x 10 x 3 mm; z 0 to 3; free margin"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q:\n%s", want, text)
		}
	}
}

func TestCameraTextNamesTheViewAndTheSide(t *testing.T) {
	vec := func(x, y, z float64) any { return []any{x, y, z} }
	for _, c := range []struct {
		dir  any
		want string
	}{
		{vec(-0.5774, 0.5774, -0.5774), "(-0.58, 0.58, -0.58): Isometric, from above, from +x and -y"},
		{vec(0, 1, 0), "(0, 1, 0): Front, from -y"},
		{vec(0, 0, -1), "(0, 0, -1): Top, from above"},
		{vec(-1, 0, 0), "(-1, 0, 0): Right, from +x"},
		{vec(0, -1, 0), "(0, -1, 0): Back, from +y"},
		{vec(1, 0, 0), "(1, 0, 0): Left, from -x"},
		{vec(0, 0, 1), "(0, 0, 1): Bottom, from below"},
		{vec(-0.3333, 0.8819, -0.3333), "(-0.33, 0.88, -0.33): Dimetric, from above, from +x and -y"},
		{vec(-0.4096, 0.7094, -0.5736), "(-0.41, 0.71, -0.57): Trimetric, from above, from +x and -y"},
		// An orbit's direction is no standard view, and a nearly horizontal one has no vertical side.
		{vec(0.6, 0.8, -0.05), "(0.6, 0.8, -0.05): from -x and -y"},
		{vec(0, 0, 0), "(0, 0, 0)"},
		{"?", "?"},
	} {
		if got := cameraText(c.dir); got != c.want {
			t.Errorf("cameraText(%v) = %q, want %q", c.dir, got, c.want)
		}
	}
}
