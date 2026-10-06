//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestHollowPartJobFollowsTheFeedbackOfTheTools rebuilds a hollow lid box: a fillet of the vertical edges
// hides its source and lists its edges, the cavity cut hides its inputs, list_subelements marks the edges
// on the plate, get_view says the image size, and check_printability gives the margin to each side.
func TestHollowPartJobFollowsTheFeedbackOfTheTools(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EHollow")
	const doc = "E2EHollow"
	create := func(name, typ string, props map[string]any) reply {
		return call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": name, "obj_type": typ, "obj_properties": props, "include_screenshot": false})
	}
	visible := func(names ...string) string {
		code := "d = FreeCAD.getDocument('" + doc + "')\n"
		for _, n := range names {
			code += "print('" + n + "', d.getObject('" + n + "').ViewObject.Visibility)\n"
		}
		r := call(t, cs, "execute_code", map[string]any{"code": code, "include_screenshot": false})
		must(t, r)
		return r.text
	}

	must(t, create("Outer", "Part::Box", map[string]any{"Length": 60, "Width": 40, "Height": 30}))
	edges := call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": "Outer", "kind": "edges"})
	must(t, edges, "on the bottom")
	var vertical []string
	for _, line := range strings.Split(edges.text, "\n") {
		if strings.Contains(line, "along z") {
			vertical = append(vertical, strings.Fields(strings.Trim(line, "| "))[0])
		}
	}
	if len(vertical) != 4 {
		t.Fatalf("vertical edges = %v", vertical)
	}

	must(t, create("Round", "Part::Fillet", map[string]any{"Base": "Outer", "Edges": vertical, "Radius": 3}),
		"Edges now: "+vertical[0]+" r3", "Hidden: Outer (inputs of Round).")
	if got := visible("Outer", "Round"); !strings.Contains(got, "Outer False") || !strings.Contains(got, "Round True") {
		t.Fatalf("visibility = %s", got)
	}
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Round", "obj_properties": map[string]any{"Radius": 4}, "include_screenshot": false}),
		"Edges now: "+vertical[0]+" r4")

	must(t, create("Inner", "Part::Box", map[string]any{"Length": 56, "Width": 36, "Height": 30, "Placement": map[string]any{"Base": map[string]any{"x": 2, "y": 2, "z": 2}}}))
	must(t, create("Hollow", "Part::Cut", map[string]any{"Base": "Round", "Tool": "Inner"}))
	if got := visible("Round", "Inner", "Hollow"); !strings.Contains(got, "Round False") || !strings.Contains(got, "Inner False") || !strings.Contains(got, "Hollow True") {
		t.Fatalf("visibility after the cut = %s", got)
	}

	// An update that does not change the source link hides nothing.
	r := call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Round", "obj_properties": map[string]any{"Radius": 4}, "include_screenshot": false})
	if strings.Contains(r.text, "Hidden:") {
		t.Fatalf("update hid something:\n%s", r.text)
	}

	// A fillet of every edge of a box leaves edges with no length.
	must(t, create("Cube", "Part::Box", map[string]any{"Length": 20, "Width": 20, "Height": 20}))
	all := call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": "Cube", "kind": "edges"})
	var names []string
	for _, line := range strings.Split(all.text, "\n") {
		if strings.HasPrefix(line, "| Edge") {
			names = append(names, strings.Fields(strings.Trim(line, "| "))[0])
		}
	}
	must(t, create("Soft", "Part::Fillet", map[string]any{"Base": "Cube", "Edges": names, "Radius": 3}))
	must(t, call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": "Soft", "kind": "edges"}),
		"degenerate (no length, a pole of a rounded corner): never pick it", "on the bottom")

	must(t, call(t, cs, "check_printability", map[string]any{"doc_name": doc, "object_names": []string{"Hollow"}, "bed_x": 80, "bed_y": 60, "bed_z": 50}),
		"free margin to the plate edges 0 mm", "z max 20")

	must(t, call(t, cs, "get_view", map[string]any{"width": 800}), "Image: 800 x ")

	fails(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "print('a')\nprint('b')\nprint('c')\nraise ValueError('late')"}),
		"Output before the error:", "a\nb\nc", "Traceback:", "ValueError: late")
}

// TestSetViewNamesTheModeItStopped: a static set_view stops a running orbit and says so.
func TestSetViewNamesTheModeItStopped(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EStop")
	must(t, call(t, cs, "create_object", map[string]any{"doc_name": "E2EStop", "obj_name": "Box", "obj_type": "Part::Box", "include_screenshot": false}))
	must(t, call(t, cs, "set_view", map[string]any{"doc_name": "E2EStop", "mode": "orbit", "include_screenshot": false}), "Orbiting")
	must(t, call(t, cs, "set_view", map[string]any{"doc_name": "E2EStop", "mode": "static", "include_screenshot": false}), "Stopped the running orbit.")
}
