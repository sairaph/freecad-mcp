//go:build e2e

package e2e

import "testing"

// TestFilletAndChamferEdgesThroughTheTools: Part::Fillet and Part::Chamfer take their edges in
// obj_properties, at creation and in update_object; a wrong edge, an expression and a missing
// edge list are refused with text that says what to do.
func TestFilletAndChamferEdgesThroughTheTools(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EFillet")
	must(t, call(t, cs, "create_object", map[string]any{"doc_name": "E2EFillet", "obj_name": "Box", "obj_type": "Part::Box", "include_screenshot": false}))

	must(t, call(t, cs, "create_object", map[string]any{"doc_name": "E2EFillet", "obj_name": "Round", "obj_type": "Part::Fillet",
		"obj_properties": map[string]any{"Base": "Box", "Edges": []string{"Edge1", "Edge2"}, "Radius": 2}, "include_screenshot": false}), "object_name: Round")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `o = FreeCAD.getDocument('E2EFillet').Round
print('edges', o.Edges, o.State)`}), "edges [(1, 2.0, 2.0), (2, 2.0, 2.0)] ['Up-to-date']")

	// A new size alone resizes the listed edges; a list of entries replaces them.
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": "E2EFillet", "obj_name": "Round", "obj_properties": map[string]any{"Radius": 1.5}, "include_screenshot": false}))
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": "E2EFillet", "obj_name": "Round", "include_screenshot": false,
		"obj_properties": map[string]any{"Edges": []map[string]any{{"edge": "Edge3", "radius": 1}, {"edge": "Edge4", "radius": 3}}}}))
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `print('edges', FreeCAD.getDocument('E2EFillet').Round.Edges)`}),
		"edges [(3, 1.0, 1.0), (4, 3.0, 3.0)]")

	must(t, call(t, cs, "create_object", map[string]any{"doc_name": "E2EFillet", "obj_name": "Cut", "obj_type": "Part::Chamfer",
		"obj_properties": map[string]any{"Base": "Box", "Edges": []map[string]any{{"edge": "Edge1", "size": 1}, {"edge": "Edge5", "size": 2}}}, "include_screenshot": false}), "object_name: Cut")

	fails(t, call(t, cs, "update_object", map[string]any{"doc_name": "E2EFillet", "obj_name": "Round", "include_screenshot": false,
		"obj_properties": map[string]any{"Edges": []string{"Edge99"}, "Radius": 1}}), "Edge 'Edge99' does not exist on 'Box'", "Edge1 to Edge12", "list_subelements")
	fails(t, call(t, cs, "update_object", map[string]any{"doc_name": "E2EFillet", "obj_name": "Round", "include_screenshot": false,
		"obj_properties": map[string]any{"Edges": []string{"Edge1"}, "Radius": "=Params.r"}}), "cannot be an expression")

	// Without edges FreeCAD cannot compute the object: it is refused and nothing is left behind.
	fails(t, call(t, cs, "create_object", map[string]any{"doc_name": "E2EFillet", "obj_name": "Bare", "obj_type": "Part::Fillet",
		"obj_properties": map[string]any{"Base": "Box"}, "include_screenshot": false}), "needs Base and Edges", "Nothing was created")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": `print('has', FreeCAD.getDocument('E2EFillet').getObject('Bare'))`}), "has None")
}

// TestListSubelementsGivesEdgeEnds: an edge row has its start and end, and a line parallel to an
// axis says which, so vertical and horizontal edges and top and bottom edges tell apart.
func TestListSubelementsGivesEdgeEnds(t *testing.T) {
	cs := session(t)
	newDoc(t, cs, "E2EEdges")
	must(t, call(t, cs, "create_object", map[string]any{"doc_name": "E2EEdges", "obj_name": "Box", "obj_type": "Part::Box", "include_screenshot": false}))
	r := call(t, cs, "list_subelements", map[string]any{"doc_name": "E2EEdges", "obj_name": "Box", "kind": "edges"})
	must(t, r, "| Edge1 | line | 10 | from (0, 0, 0) to (0, 0, 10), along z |", "along x", "along y")
	must(t, call(t, cs, "create_object", map[string]any{"doc_name": "E2EEdges", "obj_name": "Cyl", "obj_type": "Part::Cylinder", "include_screenshot": false}))
	must(t, call(t, cs, "list_subelements", map[string]any{"doc_name": "E2EEdges", "obj_name": "Cyl", "kind": "edges"}), "circle", "radius 2, center")
}
