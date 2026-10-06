package mcpserver

import (
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/xmlrpc/xmlrpctest"
)

func TestListSubelementsSendsTheFiltersAndKeepsTheTotals(t *testing.T) {
	var sent []any
	fc := addon(t, map[string]xmlrpctest.Handler{"list_subelements": func(args []any) (any, error) {
		sent = args
		return map[string]any{"success": true, "edge_count": int64(1294), "filters": map[string]any{"along": "z"},
			"edges": []any{map[string]any{"name": "Edge4", "curve": "line", "length": 10.0, "start": []any{0.0, 0.0, 0.0}, "end": []any{0.0, 0.0, 10.0}, "along": "z"}}}, nil
	}})
	cs := session(t, settingsFor(fc))
	res := call(t, cs, "list_subelements", map[string]any{"doc_name": "D", "obj_name": "Knob", "kind": "edges", "along": "z", "min_length": 2.5, "smooth": false})
	text := replyText(res)
	for _, want := range []string{"edges: 1294", "edges_matched: 1", "| Edge4 | line |"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q:\n%s", want, text)
		}
	}
	filters, _ := sent[3].(map[string]any)
	if filters["along"] != "z" || filters["min_length"] != 2.5 || filters["smooth"] != false || len(filters) != 3 {
		t.Errorf("filters sent = %v", sent)
	}
}

func TestListSubelementsWithoutFiltersIsUnchanged(t *testing.T) {
	var sent []any
	fc := addon(t, map[string]xmlrpctest.Handler{"list_subelements": func(args []any) (any, error) {
		sent = args
		return map[string]any{"success": true, "face_count": int64(1), "faces": []any{map[string]any{"name": "Face1", "surface": "plane", "area": 4.0, "center": []any{0.0, 0.0, 0.0}}}}, nil
	}})
	text := replyText(call(t, session(t, settingsFor(fc)), "list_subelements", map[string]any{"doc_name": "D", "obj_name": "Box"}))
	if strings.Contains(text, "matched") || !strings.Contains(text, "faces: 1") {
		t.Errorf("reply = %s", text)
	}
	if len(sent) != 3 {
		t.Errorf("an addon from before the filters takes three arguments, got %v", sent)
	}
}

func TestListSubelementsSaysWhenNothingMatches(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{"list_subelements": func([]any) (any, error) {
		return map[string]any{"success": true, "edge_count": int64(12), "filters": map[string]any{"curve": "circle"}, "edges": []any{}}, nil
	}})
	text := replyText(call(t, session(t, settingsFor(fc)), "list_subelements", map[string]any{"doc_name": "D", "obj_name": "Box", "kind": "edges", "curve": "circle"}))
	if !strings.Contains(text, "No edges match the filters (the shape has 12).") || strings.Contains(text, "The shape has no edges") {
		t.Errorf("reply = %s", text)
	}
}

func TestListSubelementsRefusesAFilterValueItDoesNotHave(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{})
	res := call(t, session(t, settingsFor(fc)), "list_subelements", map[string]any{"doc_name": "D", "obj_name": "Box", "curve": "arc"})
	if !res.IsError {
		t.Errorf("curve arc was accepted: %s", replyText(res))
	}
}

func TestMeshToSolidSaysWhatRefineDidAndThatTheMeshStaysVisible(t *testing.T) {
	reply := func(refined, visible bool) func([]any) (any, error) {
		return func([]any) (any, error) {
			return map[string]any{"success": true, "object": "Knob_mesh", "created_object": map[string]any{"name": "Knob_solid", "label": "Knob_solid", "type": "Part::Feature"},
				"is_solid": true, "faces": int64(428), "volume": 4177.0, "refined": refined, "source_visible": visible, "warnings": []any{}}, nil
		}
	}
	fc := addon(t, map[string]xmlrpctest.Handler{"mesh_to_solid": reply(true, true)})
	text := replyText(call(t, session(t, settingsFor(fc)), "mesh_to_solid", map[string]any{"doc_name": "D", "obj_name": "Knob_mesh", "refine": true, "include_screenshot": false}))
	for _, want := range []string{"428 face(s)", "Refine merged only flat regions: curved surfaces keep one face per triangle", "fillet and chamfer edges on them are not practical",
		"The source mesh 'Knob_mesh' stays visible.", "before check_printability or exporting the default set"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q:\n%s", want, text)
		}
	}
	fc = addon(t, map[string]xmlrpctest.Handler{"mesh_to_solid": reply(false, false)})
	text = replyText(call(t, session(t, settingsFor(fc)), "mesh_to_solid", map[string]any{"doc_name": "D", "obj_name": "Knob_mesh", "include_screenshot": false}))
	if strings.Contains(text, "stays visible") || !strings.Contains(text, "Each triangle is a face; refine true would merge only flat regions") {
		t.Errorf("reply for a hidden source without refine = %s", text)
	}
}

func TestCreateAndUpdateObjectListTheOtherShapesTheyRebuilt(t *testing.T) {
	changed := []any{map[string]any{"name": "Body", "shape": map[string]any{"solids": int64(1), "size": []any{60.0, 40.0, 4.0}, "volume": 9600.0}}}
	fc := addon(t, map[string]xmlrpctest.Handler{
		"create_object": func([]any) (any, error) {
			return map[string]any{"success": true, "object_name": "Pad", "changed_shapes": changed, "changed_shapes_count": int64(1)}, nil
		},
		"edit_object": func([]any) (any, error) {
			return map[string]any{"success": true, "object_name": "Sk", "changed_shapes": changed, "changed_shapes_count": int64(1)}, nil
		},
	})
	cs := session(t, settingsFor(fc))
	for name, text := range map[string]string{
		"create": replyText(call(t, cs, "create_object", map[string]any{"doc_name": "D", "obj_name": "Pad", "obj_type": "PartDesign::Pad", "include_screenshot": false})),
		"update": replyText(call(t, cs, "update_object", map[string]any{"doc_name": "D", "obj_name": "Sk", "obj_properties": map[string]any{"Length": 1}, "include_screenshot": false})),
	} {
		if !strings.Contains(text, "Shapes now: Body: 1 solid, 60 x 40 x 4 mm, volume 9600.0 mm^3") {
			t.Errorf("%s reply lacks the shape:\n%s", name, text)
		}
	}
}
