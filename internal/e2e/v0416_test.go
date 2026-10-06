//go:build e2e

package e2e

import (
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func v0416Create(t *testing.T, cs *mcp.ClientSession, doc, name, typ string, props map[string]any) reply {
	t.Helper()
	return call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": name, "obj_type": typ, "obj_properties": props, "include_screenshot": false})
}

func v0416Code(t *testing.T, cs *mcp.ClientSession, doc, code string, contains ...string) {
	t.Helper()
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "d = FreeCAD.getDocument('" + doc + "')\n" + code}), contains...)
}

// TestSignedQuantitiesInCellsStayQuantities: "-20 deg" and "-20 mm" are stored as expressions and drive a Placement
// angle and a length; explicit text stays text; an unknown unit stays text.
func TestSignedQuantitiesInCellsStayQuantities(t *testing.T) {
	cs := session(t)
	const doc = "E2ESigned"
	newDoc(t, cs, doc)
	must(t, v0416Create(t, cs, doc, "Params", "Spreadsheet::Sheet", nil))
	cell := func(address, content, alias string) map[string]any {
		c := map[string]any{"cell": address, "content": content}
		if alias != "" {
			c["alias"] = alias
		}
		return c
	}
	r := call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{
		cell("A1", "-20 deg", "lean"), cell("A2", "-20 mm", "back"), cell("A3", "- 5 mm", ""), cell("A4", "'-20 mm", ""),
		cell("A5", "20 deg", ""), cell("A6", "-20", ""), cell("A7", "-20 foo", ""), cell("A8", "+5 mm", "")}})
	must(t, r, "| A1 | lean | =-20 deg |", "| A2 | back | =-20 mm |", "| A3 |  | =-5 mm |", "| A4 |  | '-20 mm |", "| A5 |  | =20 deg |",
		"| A6 |  | -20 |", "| A7 |  | '-20 foo |", "| A8 |  | =+5 mm |")

	must(t, v0416Create(t, cs, doc, "Plate", "Part::Box", map[string]any{"Length": 20, "Width": 2, "Height": 10}))
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Plate", "include_screenshot": false,
		"obj_properties": map[string]any{"Placement.Rotation.Axis.x": 1, "Placement.Rotation.Angle": "=Params.lean", "Placement.Base.y": "=Params.back"}}))
	v0416Code(t, cs, doc, "p = d.getObject('Plate').Placement\nprint('angle', round(p.Rotation.Angle * 180 / 3.141592653589793, 3), 'axis', [round(a, 3) for a in p.Rotation.Axis], 'y', p.Base.y)",
		"y -20.0")
}

// TestRotationSignFollowsTheRightHandRule: about x a negative angle leans a plate in the xz plane back (top toward +y).
func TestRotationSignFollowsTheRightHandRule(t *testing.T) {
	cs := session(t)
	const doc = "E2ERotation"
	newDoc(t, cs, doc)
	rot := func(x, y, z, angle float64) map[string]any {
		return map[string]any{"Rotation": map[string]any{"Axis": map[string]any{"x": x, "y": y, "z": z}, "Angle": angle}}
	}
	must(t, v0416Create(t, cs, doc, "AboutX", "Part::Box", map[string]any{"Length": 20, "Width": 2, "Height": 10, "Placement": rot(1, 0, 0, -30)}))
	must(t, v0416Create(t, cs, doc, "AboutY", "Part::Box", map[string]any{"Length": 2, "Width": 2, "Height": 10, "Placement": rot(0, 1, 0, 30)}))
	must(t, v0416Create(t, cs, doc, "AboutZ", "Part::Box", map[string]any{"Length": 10, "Width": 2, "Height": 2, "Placement": rot(0, 0, 1, 30)}))
	v0416Code(t, cs, doc, "def pt(n, v):\n    r = d.getObject(n).Placement.Rotation.multVec(FreeCAD.Vector(*v))\n    return [round(c, 3) for c in r]\n"+
		"print('x', pt('AboutX', (0, 0, 10)), 'y', pt('AboutY', (0, 0, 10)), 'z', pt('AboutZ', (10, 0, 0)))",
		"x [0.0, 5.0, 8.66]", "y [5.0, 0.0, 8.66]", "z [8.66, 5.0, 0.0]")
}

// TestFailedRoundingsAndTheirDependentsSayWhatToDo: a fillet whose Base lost an edge, a radius that does not fit, and a
// Cut that only waits for the failed fillet.
func TestFailedRoundingsAndTheirDependentsSayWhatToDo(t *testing.T) {
	cs := session(t)
	const doc = "E2EFailed"
	newDoc(t, cs, doc)
	must(t, v0416Create(t, cs, doc, "Box", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 10}))
	must(t, v0416Create(t, cs, doc, "Fil", "Part::Fillet", map[string]any{"Base": "Box", "Edges": []any{"Edge1", "Edge7"}, "Radius": 1}))
	must(t, v0416Create(t, cs, doc, "Cyl", "Part::Cylinder", map[string]any{"Radius": 5, "Height": 10}))
	must(t, v0416Create(t, cs, doc, "Final", "Part::Cut", map[string]any{"Base": "Fil", "Tool": "Cyl"}))

	// The Base changes to a part with fewer edges, as a parameter change can do: Edge7 is gone.
	v0416Code(t, cs, doc, "f = d.getObject('Fil')\nf.Base = d.getObject('Cyl')\nf.Edges = [(1, 1.0, 1.0), (7, 1.0, 1.0)]\nd.getObject('Final').touch()\nd.recompute()")
	rec := call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false})
	must(t, rec, "Fil (Part::Fillet): NCollection_IndexedMap::FindKey.",
		"An edge it rounds no longer exists in Cyl after the change. Call list_subelements on Cyl and set Edges again.",
		"Final (Part::Cut): waits for Fil, which failed.", `"obj_name": "Fil"`)

	// A radius that does not fit the edges.
	r := v0416Create(t, cs, doc, "Big", "Part::Fillet", map[string]any{"Base": "Box", "Edges": []any{"Edge1", "Edge2", "Edge3", "Edge4"}, "Radius": 50})
	fails(t, r, "BRep_API: command not done", "The radius is probably too large for these edges: try a smaller radius or fewer edges, and leave out degenerate edges.")
	r = v0416Create(t, cs, doc, "BigChamfer", "Part::Chamfer", map[string]any{"Base": "Box", "Edges": []any{"Edge1", "Edge2"}, "Size": 50})
	fails(t, r, "The size is probably too large for these edges: try a smaller size or fewer edges, and leave out degenerate edges.")

	// The boundary edge of a rounded edge joins two faces smoothly: FreeCAD finds no suitable edge.
	must(t, v0416Create(t, cs, doc, "One", "Part::Fillet", map[string]any{"Base": "Box", "Edges": []any{"Edge1"}, "Radius": 3}))
	r = v0416Create(t, cs, doc, "OnTangent", "Part::Fillet", map[string]any{"Base": "One", "Edges": []any{"Edge2"}, "Radius": 0.5})
	fails(t, r, "There are no suitable edges for chamfer or fillet. An edge in Edges cannot be rounded: call list_subelements on One and leave out edges marked degenerate or smooth.")
}

// TestBoxEdgeNamesDoNotChangeWithItsSize backs the guide's advice to round primitives before booleans.
func TestBoxEdgeNamesDoNotChangeWithItsSize(t *testing.T) {
	cs := session(t)
	const doc = "E2EEdges"
	newDoc(t, cs, doc)
	must(t, v0416Create(t, cs, doc, "Box", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 10}))
	must(t, v0416Create(t, cs, doc, "Cyl", "Part::Cylinder", map[string]any{"Radius": 5, "Height": 10}))
	code := "def sig(n):\n    return [(round(e.tangentAt(e.FirstParameter).x, 2), round(e.tangentAt(e.FirstParameter).y, 2), round(e.tangentAt(e.FirstParameter).z, 2)) for e in d.getObject(n).Shape.Edges]\n"
	v0416Code(t, cs, doc, code+"a = (sig('Box'), sig('Cyl'))\nd.getObject('Box').Length = 33\nd.getObject('Box').Height = 4\nd.getObject('Cyl').Radius = 2\nd.getObject('Cyl').Height = 30\nd.recompute()\nprint('same edges', a == (sig('Box'), sig('Cyl')))", "same edges True")
}

// TestPrintabilityWithoutBedZSaysTheHeightWasNotChecked.
func TestPrintabilityWithoutBedZSaysTheHeightWasNotChecked(t *testing.T) {
	cs := session(t)
	const doc = "E2EBedZ"
	newDoc(t, cs, doc)
	must(t, v0416Create(t, cs, doc, "Box", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 10}))
	r := call(t, cs, "check_printability", map[string]any{"doc_name": doc, "bed_x": 200, "bed_y": 200})
	must(t, r, "Height not checked: pass bed_z with the printer's build height.")
	if r2 := call(t, cs, "check_printability", map[string]any{"doc_name": doc, "bed_x": 200, "bed_y": 200, "bed_z": 250}); strings.Contains(r2.text, "Height not checked") {
		t.Fatalf("reply with bed_z:\n%s", r2.text)
	}
}

// TestSmoothEdgesAndSeamsAreMarked: the boundary edges of a fillet and a cylinder's seam are marked, a sharp box edge
// is not, and a box with every edge rounded has smooth and degenerate edges only.
func TestSmoothEdgesAndSeamsAreMarked(t *testing.T) {
	cs := session(t)
	const doc = "E2ESmoothEdges"
	newDoc(t, cs, doc)
	must(t, v0416Create(t, cs, doc, "Box", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 10}))
	must(t, v0416Create(t, cs, doc, "Cyl", "Part::Cylinder", map[string]any{"Radius": 5, "Height": 10}))
	must(t, v0416Create(t, cs, doc, "One", "Part::Fillet", map[string]any{"Base": "Box", "Edges": []any{"Edge1"}, "Radius": 3}))
	all := make([]any, 12)
	for i := range all {
		all[i] = "Edge" + strconv.Itoa(i+1)
	}
	must(t, v0416Create(t, cs, doc, "Soft", "Part::Fillet", map[string]any{"Base": "Box", "Edges": all, "Radius": 3}))
	edges := func(name string) string {
		r := call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": name, "kind": "edges"})
		must(t, r)
		return r.text
	}
	count := func(text, what string) int { return strings.Count(text, what) }
	const smooth, seam, degenerate = "smooth (its two faces", "smooth (a seam", "degenerate ("

	box := edges("Box")
	if count(box, "smooth") != 0 || count(box, "| Edge") != 12 {
		t.Fatalf("a box has no smooth edge:\n%s", box)
	}
	cyl := edges("Cyl")
	if count(cyl, seam) != 1 || count(cyl, smooth) != 0 {
		t.Fatalf("a cylinder has one seam:\n%s", cyl)
	}
	one := edges("One")
	if count(one, smooth) != 2 || count(one, seam) != 0 || count(one, degenerate) != 0 {
		t.Fatalf("one rounded edge has two smooth boundary edges:\n%s", one)
	}
	soft := edges("Soft")
	if count(soft, smooth) != 48 || count(soft, degenerate) != 8 || count(soft, "| Edge") != 56 {
		t.Fatalf("every edge rounded: want 48 smooth, 8 degenerate of 56:\n%s", soft)
	}
}
