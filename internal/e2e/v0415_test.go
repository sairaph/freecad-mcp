//go:build e2e

package e2e

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
)

func v0415Create(t *testing.T, cs *mcp.ClientSession, doc, name, typ string, props map[string]any) reply {
	t.Helper()
	return call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": name, "obj_type": typ, "obj_properties": props, "include_screenshot": false})
}

func v0415At(x, y, z float64) map[string]any {
	return map[string]any{"Base": map[string]any{"x": x, "y": y, "z": z}}
}

// TestNoOpCutsAndEmptyResultsAreReportedAfterTheFact: a Cut by a far box warns on create and update, recompute_document
// lists the no-op Cut and an empty Common (not "cleanly"), the compact list has a Solids column and every parent of a
// shared tool, and a recompute with nothing to do says so.
func TestNoOpCutsAndEmptyResultsAreReportedAfterTheFact(t *testing.T) {
	cs := session(t)
	const doc = "E2ENoOp"
	newDoc(t, cs, doc)
	box := map[string]any{"Length": 10, "Width": 10, "Height": 10}
	must(t, v0415Create(t, cs, doc, "Block", "Part::Box", box))
	must(t, v0415Create(t, cs, doc, "Hit", "Part::Box", map[string]any{"Length": 4, "Width": 4, "Height": 4, "Placement": v0415At(2, 2, 2)}))
	farProps := map[string]any{"Length": 10, "Width": 10, "Height": 10, "Placement": v0415At(40, 0, 0)}
	must(t, v0415Create(t, cs, doc, "Far", "Part::Box", farProps))

	r := v0415Create(t, cs, doc, "Good", "Part::Cut", map[string]any{"Base": "Block", "Tool": "Hit"})
	must(t, r)
	if strings.Contains(r.text, "Warning:") {
		t.Fatalf("a cut that removes material warned:\n%s", r.text)
	}
	must(t, v0415Create(t, cs, doc, "NoCut", "Part::Cut", map[string]any{"Base": "Block", "Tool": "Far"}),
		"Warning: The tool does not reach the base: nothing was removed. Check their Placement.")
	must(t, v0415Create(t, cs, doc, "Fz", "Part::Fuse", map[string]any{"Base": "Hit", "Tool": "Far"}))
	must(t, v0415Create(t, cs, doc, "EmptyCommon", "Part::Common", map[string]any{"Base": "Block", "Tool": "Far"}),
		"Warning: The result holds no solid: its inputs do not overlap.")

	// An update that moves a cut tool away warns too.
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Good", "obj_properties": map[string]any{"Tool": "Far"}, "include_screenshot": false}),
		"Warning: The tool does not reach the base: nothing was removed.")

	rec := call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false})
	must(t, rec, "Empty or no-op results:", "NoCut (the tool does not reach the base)", "EmptyCommon (no solid: its inputs do not overlap)", "empty_count: 3")
	if strings.Contains(rec.text, "cleanly") {
		t.Fatalf("a document with empty results recomputed cleanly:\n%s", rec.text)
	}
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false}),
		"no object needed a recompute; 7 object(s), none invalid.")

	list := call(t, cs, "list_objects", map[string]any{"doc_name": doc, "compact": true})
	must(t, list, "| Parent | Solids | Visible |", "| Far | Far | Part::Box | Up-to-date | true | Good, NoCut, Fz, EmptyCommon | 1 |",
		"| EmptyCommon | EmptyCommon | Part::Common | Up-to-date | true |  | 0 |")
}

// TestTeardropRecipesAlongXAndY builds the teardrop tool the guide describes and checks its tight box, that it is
// one solid and that its sides are tangent to the circle (its section is 3/4 pi r^2 + r^2).
func TestTeardropRecipesAlongXAndY(t *testing.T) {
	cs := session(t)
	const doc = "E2ETeardrop"
	newDoc(t, cs, doc)
	rot := func(x, y, z, angle float64) map[string]any {
		return map[string]any{"Axis": map[string]any{"x": x, "y": y, "z": z}, "Angle": angle}
	}
	place := func(base [3]float64, rotation map[string]any) map[string]any {
		return map[string]any{"Base": map[string]any{"x": base[0], "y": base[1], "z": base[2]}, "Rotation": rotation}
	}
	// Hole axis along x through (0, 10, 20) and along y through (30, 0, 20); radius 3, length 25.
	must(t, v0415Create(t, cs, doc, "CylX", "Part::Cylinder", map[string]any{"Radius": 3, "Height": 25, "Placement": place([3]float64{0, 10, 20}, rot(0, 1, 0, 90))}))
	must(t, v0415Create(t, cs, doc, "BoxX", "Part::Box", map[string]any{"Length": 25, "Width": 3, "Height": 3, "Placement": place([3]float64{0, 10, 20}, rot(1, 0, 0, 45))}))
	must(t, v0415Create(t, cs, doc, "ToolX", "Part::Fuse", map[string]any{"Base": "CylX", "Tool": "BoxX"}))
	must(t, v0415Create(t, cs, doc, "CylY", "Part::Cylinder", map[string]any{"Radius": 3, "Height": 25, "Placement": place([3]float64{30, 0, 20}, rot(1, 0, 0, -90))}))
	must(t, v0415Create(t, cs, doc, "BoxY", "Part::Box", map[string]any{"Length": 3, "Width": 25, "Height": 3, "Placement": place([3]float64{30, 0, 20}, rot(0, 1, 0, -45))}))
	must(t, v0415Create(t, cs, doc, "ToolY", "Part::Fuse", map[string]any{"Base": "CylY", "Tool": "BoxY"}))

	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "import math\nd = FreeCAD.getDocument('" + doc + "')\n" +
		"for n in ('ToolX', 'ToolY'):\n    s = d.getObject(n).Shape.removeSplitter()\n    b = s.optimalBoundingBox(False, False)\n" +
		"    print(n, len(s.Solids), round(b.XMin, 3), round(b.XMax, 3), round(b.YMin, 3), round(b.YMax, 3), round(b.ZMin, 3), round(b.ZMax, 3), round(s.Volume / 25, 3))\n" +
		"print('want height', round(3 + 3 * math.sqrt(2), 3), 'section', round(0.75 * math.pi * 9 + 9, 3))"})
	must(t, r, " 25.0 7.0 13.0 17.0 24.243 30.206", "ToolY 1 27.0 33.0 ", " 25.0 17.0 24.243 30.206", "want height 7.243 section 30.206")
}

// TestWedgeAndPrismRecipes: the primitives the guide names for triangles and regular polygons.
func TestWedgeAndPrismRecipes(t *testing.T) {
	cs := session(t)
	const doc = "E2EWedge"
	newDoc(t, cs, doc)
	wedge := map[string]any{"Xmin": 0, "Xmax": 30, "Ymin": 0, "Ymax": 12, "Zmin": 0, "Zmax": 4, "X2min": 0, "X2max": 0, "Z2min": 0, "Z2max": 4}
	must(t, v0415Create(t, cs, doc, "Gusset", "Part::Wedge", wedge), "Shape: 1 solid, 30 x 12 x 4 mm at x 0 to 30, y 0 to 12, z 0 to 4, volume 720.0 mm^3")
	stood := map[string]any{"Placement": map[string]any{"Base": map[string]any{"x": 0, "y": 4, "z": 0}, "Rotation": map[string]any{"Axis": map[string]any{"x": 1, "y": 0, "z": 0}, "Angle": 90}}}
	for k, v := range wedge {
		stood[k] = v
	}
	must(t, v0415Create(t, cs, doc, "Stood", "Part::Wedge", stood), "Shape: 1 solid, 30 x 4 x 12 mm at ", "volume 720.0 mm^3")
	// The box and the volume cannot tell a triangle in xy from one in xz: the vertices do.
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "d = FreeCAD.getDocument('" + doc + "')\n" +
		"for n in ('Gusset', 'Stood'):\n    print(n, sorted((round(v.X, 2) + 0.0, round(v.Y, 2) + 0.0, round(v.Z, 2) + 0.0) for v in d.getObject(n).Shape.Vertexes))"}),
		"Gusset [(0.0, 0.0, 0.0), (0.0, 0.0, 4.0), (0.0, 12.0, 0.0), (0.0, 12.0, 4.0), (30.0, 0.0, 0.0), (30.0, 0.0, 4.0)]",
		"Stood [(0.0, 0.0, 0.0), (0.0, 0.0, 12.0), (0.0, 4.0, 0.0), (0.0, 4.0, 12.0), (30.0, 0.0, 0.0), (30.0, 4.0, 0.0)]")
	must(t, v0415Create(t, cs, doc, "Hex", "Part::Prism", map[string]any{"Polygon": 6, "Circumradius": 10, "Height": 5}),
		"Shape: 1 solid, 20 x 17.32 x 5 mm")
}
