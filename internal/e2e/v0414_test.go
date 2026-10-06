//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestCreateAndUpdateReportShapeWarningAndHiddenInputs: a Cut reports its shape and the inputs FreeCAD hid, a
// Common of parts that do not overlap warns, an update that empties a Cut warns, and the compact object list is a
// table.
func TestCreateAndUpdateReportShapeWarningAndHiddenInputs(t *testing.T) {
	cs := session(t)
	const doc = "E2EShape"
	newDoc(t, cs, doc)
	create := func(name, typ string, props map[string]any) reply {
		return call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": name, "obj_type": typ, "obj_properties": props, "include_screenshot": false})
	}
	at := func(x float64) map[string]any {
		return map[string]any{"Base": map[string]any{"x": x, "y": 0, "z": 0}}
	}

	must(t, create("Block", "Part::Box", map[string]any{"Length": 40, "Width": 20, "Height": 12}), "Shape: 1 solid, 40 x 20 x 12 mm, volume 9600.0 mm^3")
	must(t, create("Hole", "Part::Cylinder", map[string]any{"Radius": 3, "Height": 12, "Placement": at(10)}))
	r := create("Clip", "Part::Cut", map[string]any{"Base": "Block", "Tool": "Hole"})
	must(t, r, "Shape: 1 solid, 40 x 20 x 12 mm, volume 9", "Hidden: Block, Hole (inputs of Clip).")
	if strings.Contains(r.text, "Warning:") {
		t.Fatalf("a cut with a solid warned:\n%s", r.text)
	}

	must(t, create("Far", "Part::Box", map[string]any{"Length": 5, "Width": 5, "Height": 5, "Placement": at(500)}))
	r = create("Nothing", "Part::Common", map[string]any{"Base": "Block", "Tool": "Far"})
	must(t, r, "Shape: no solid, nothing in it", "Warning: The result holds no solid: its inputs do not overlap. Check their Placement.")

	must(t, create("Big", "Part::Box", map[string]any{"Length": 100, "Width": 100, "Height": 100, "Placement": map[string]any{"Base": map[string]any{"x": -30, "y": -30, "z": -30}}}))
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Clip", "obj_properties": map[string]any{"Tool": "Big"}, "include_screenshot": false}),
		"Shape: no solid", "Warning: The result holds no solid: the tool removes all of the base.")

	must(t, call(t, cs, "list_objects", map[string]any{"doc_name": doc, "compact": true}),
		"| Name | Label | Type | State | Valid | Parent | Visible |", "| Block | Block | Part::Box |", "| Clip | Clip | Part::Cut |")

	// The form code.md gives for a script that builds a fillet.
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "d = FreeCAD.getDocument('" + doc + "')\n" +
		"f = d.addObject('Part::Fillet', 'Soft')\nf.Base = d.getObject('Block')\nf.Edges = [(1, 2.0, 2.0), (3, 2.0, 2.0)]\nd.recompute()\nprint('fillet', f.Shape.isValid(), len(f.Shape.Solids))"}),
		"fillet True 1")
}

// TestNoSolidWarningIgnoresFeaturesThatAreNotSolidBooleans: a sketch attached to a box face holds no solid by design
// and must not warn; a container with no feature has no Shape line.
func TestNoSolidWarningIgnoresFeaturesThatAreNotSolidBooleans(t *testing.T) {
	cs := session(t)
	const doc = "E2ENoWarn"
	newDoc(t, cs, doc)
	create := func(name, typ string, props map[string]any) reply {
		return call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": name, "obj_type": typ, "obj_properties": props, "include_screenshot": false})
	}
	must(t, create("Box", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 10}))
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "d = FreeCAD.getDocument('" + doc + "')\n" +
		"import Part\nb = d.getObject('Box')\nsk = d.addObject('Sketcher::SketchObject', 'Sketch')\nsk.AttachmentSupport = [(b, 'Face6')]\nsk.MapMode = 'FlatFace'\n" +
		"sk.addGeometry(Part.LineSegment(FreeCAD.Vector(0, 0, 0), FreeCAD.Vector(5, 0, 0)))\n" +
		"d.recompute()\n" +
		"print('sketch out', [o.Name for o in sk.OutList], 'sketch solids', len(sk.Shape.Solids))"}), "sketch out ['Box']")
	for _, name := range []string{"Sketch"} {
		r := call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": name, "obj_properties": map[string]any{"Label": name + "x"}, "include_screenshot": false})
		must(t, r)
		if strings.Contains(r.text, "Warning:") {
			t.Fatalf("%s warned:\n%s", name, r.text)
		}
	}
	r := create("Body", "PartDesign::Body", nil)
	must(t, r)
	if strings.Contains(r.text, "Shape:") {
		t.Fatalf("an empty body has a Shape line:\n%s", r.text)
	}
}
