//go:build e2e

package e2e

import (
	"math"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// docOutput runs code in the document and returns what it printed.
func docOutput(t *testing.T, cs *mcp.ClientSession, doc, code string) string {
	t.Helper()
	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false,
		"code": "import FreeCAD\nd = FreeCAD.getDocument('" + doc + "')\n" + code})
	_, out, found := strings.Cut(r.text, "Output: ")
	if !found {
		t.Fatalf("execute_code reply has no Output marker:\n%s", r.text)
	}
	return strings.TrimSpace(out)
}

// plateWithHole makes a Body with a 60 x 60 x 10 Pad centred on the origin and a Hole (a subtractive cylinder of
// radius 3, through the plate) at (-20, 0), and returns the volume of that Body.
func plateWithHole(t *testing.T, cs *mcp.ClientSession, doc string) float64 {
	t.Helper()
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "Sketch", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane",
		"Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{-30, -30}, "size": []any{60, 60}}}}}, nil))
	must(t, pdCreate(t, cs, doc, "Pad", "PartDesign::Pad", map[string]any{"Profile": "Sketch", "Length": 10}, nil), "In Body 'Body' (Tip: Pad).")
	r := pdCreate(t, cs, doc, "Hole", "PartDesign::SubtractiveCylinder", map[string]any{"Radius": 3, "Height": 10, "Placement": vec(-20, 0, 0)}, nil)
	must(t, r, "In Body 'Body' (Tip: Hole).")
	return replyVolume(t, r)
}

const holeVolume = math.Pi * 3 * 3 * 10

// TestAPatternBecomesTheTip: a box, a subtractive cylinder and a LinearPattern of it with 4 occurrences: the Tip is the pattern, the Body
// volume drops by 4 holes, no Tip warning shows, and a following feature builds on the pattern.
func TestAPatternBecomesTheTip(t *testing.T) {
	cs := session(t)
	const doc = "E2EPatternTip"
	plateWithHole(t, cs, doc)
	r := pdCreate(t, cs, doc, "Pat", "PartDesign::LinearPattern", map[string]any{"Originals": []any{"Hole"}, "Direction": "X_Axis", "Length": 30, "Occurrences": 4}, nil)
	must(t, r, "In Body 'Body' (Tip: Pat).")
	if strings.Contains(r.text, "Tip stays") {
		t.Fatalf("a pattern that is the Tip is reported as not:\n%s", r.text)
	}
	if v, want := replyVolume(t, r), 60*60*10-4*holeVolume; math.Abs(v-want) > 1 {
		t.Fatalf("the Body volume = %g, want %g (four holes)", v, want)
	}
	must(t, pdCreate(t, cs, doc, "Hole2", "PartDesign::SubtractiveCylinder", map[string]any{"Radius": 3, "Height": 10, "Placement": vec(0, 20, 0)}, nil),
		"In Body 'Body' (Tip: Hole2).")
	if got := docOutput(t, cs, doc, "print(d.Hole2.BaseFeature.Name, d.Body.Tip.Name)"); got != "Pat Hole2" {
		t.Fatalf("BaseFeature and Tip = %q, want the pattern as the base of the next feature", got)
	}
}

// TestAnOriginAxisOrPlaneGivenByNameComputes: a pattern's Direction "X_Axis", Axis "Z_Axis" and MirrorPlane "YZ_Plane" in a Body all
// compute and cut the holes they should; the other whole-object forms are accepted; a Pad's Profile "Sketch" still computes
// (made by plateWithHole); a bad value names the bare-name form.
func TestAnOriginAxisOrPlaneGivenByNameComputes(t *testing.T) {
	cs := session(t)
	const linear = "E2EAxisLinear"
	plateWithHole(t, cs, linear)
	r := pdCreate(t, cs, linear, "Pat", "PartDesign::LinearPattern", map[string]any{"Originals": []any{"Hole"}, "Direction": "X_Axis", "Length": 30, "Occurrences": 4}, nil)
	must(t, r, "In Body 'Body' (Tip: Pat).")
	for _, form := range []any{[]any{"X_Axis", ""}, []any{"X_Axis", []any{""}}} {
		must(t, call(t, cs, "update_object", map[string]any{"doc_name": linear, "obj_name": "Pat", "include_screenshot": false,
			"obj_properties": map[string]any{"Direction": form}}))
	}
	fails(t, call(t, cs, "update_object", map[string]any{"doc_name": linear, "obj_name": "Pat", "include_screenshot": false,
		"obj_properties": map[string]any{"Direction": []any{"X_Axis", "Edge1", "Edge2"}}}),
		`the bare object name for a whole object, such as an origin axis "X_Axis"`)

	const polar = "E2EAxisPolar"
	plateWithHole(t, cs, polar)
	r = pdCreate(t, cs, polar, "Pat", "PartDesign::PolarPattern", map[string]any{"Originals": []any{"Hole"}, "Axis": "Z_Axis", "Angle": 360, "Occurrences": 6}, nil)
	must(t, r, "In Body 'Body' (Tip: Pat).")
	if v, want := replyVolume(t, r), 60*60*10-6*holeVolume; math.Abs(v-want) > 1 {
		t.Fatalf("polar volume = %g, want %g (six holes)", v, want)
	}

	const mirrored = "E2EAxisMirrored"
	plateWithHole(t, cs, mirrored)
	r = pdCreate(t, cs, mirrored, "Mir", "PartDesign::Mirrored", map[string]any{"Originals": []any{"Hole"}, "MirrorPlane": "YZ_Plane"}, nil)
	must(t, r, "In Body 'Body' (Tip: Mir).")
	if v, want := replyVolume(t, r), 60*60*10-2*holeVolume; math.Abs(v-want) > 1 {
		t.Fatalf("mirrored volume = %g, want %g (two holes)", v, want)
	}
}

// TestALinearPatternHasASecondRow: Direction2, Length2 and Occurrences2 give a grid (the guide names them only while this passes).
func TestALinearPatternHasASecondRow(t *testing.T) {
	cs := session(t)
	const doc = "E2EPatternRows"
	plateWithHole(t, cs, doc)
	r := pdCreate(t, cs, doc, "Pat", "PartDesign::LinearPattern", map[string]any{"Originals": []any{"Hole"}, "Direction": "X_Axis", "Length": 30, "Occurrences": 4,
		"Direction2": "Y_Axis", "Length2": 20, "Occurrences2": 2}, nil)
	must(t, r, "In Body 'Body' (Tip: Pat).")
	if v, want := replyVolume(t, r), 60*60*10-8*holeVolume; math.Abs(v-want) > 1 {
		t.Fatalf("grid volume = %g, want %g (eight holes)", v, want)
	}
}

// TestShapeStringMakesTextThatFusesToAPart: the default font gives faces and a box about Size high; its extrusion fused to a box is one
// valid solid; the reply names the font and says the text is flat; a FontFile that does not exist is refused and creates nothing.
func TestShapeStringMakesTextThatFusesToAPart(t *testing.T) {
	cs := session(t)
	const doc = "E2EShapeString"
	newDoc(t, cs, doc)
	r := pdCreate(t, cs, doc, "Label", "Draft::ShapeString", map[string]any{"String": "PSU 12V", "Size": 8, "Placement": vec(5, 5, 2)}, nil)
	must(t, r, "Note: Font: ", "Draft::ShapeString is a flat profile")
	text := frontValue(r.text, "object_name")
	if strings.Contains(r.text, "Warning:") {
		t.Fatalf("flat text carries a no-solid warning:\n%s", r.text)
	}
	if got := docOutput(t, cs, doc, "s = d.getObject('"+text+"').Shape\nb = s.BoundBox\nprint(len(s.Faces) > 0, 6.4 <= b.YLength <= 11.2, 20 <= b.XLength <= 60)"); got != "True True True" {
		t.Fatalf("the text has faces and a box about Size high: %q", got)
	}
	must(t, pdCreate(t, cs, doc, "Plate", "Part::Box", map[string]any{"Length": 60, "Width": 20, "Height": 2}, nil))
	must(t, pdCreate(t, cs, doc, "Raised", "Part::Extrusion", map[string]any{"Base": text, "Dir": map[string]any{"x": 0, "y": 0, "z": 0.6}, "Solid": true}, nil))
	must(t, pdCreate(t, cs, doc, "Plaque", "Part::Fuse", map[string]any{"Base": "Plate", "Tool": "Raised"}, nil), "Shape: 1 solid")
	if got := docOutput(t, cs, doc, "print(d.Plaque.Shape.isValid(), len(d.Plaque.Shape.Solids))"); got != "True 1" {
		t.Fatalf("the plaque is one valid solid: %q", got)
	}

	names := "print(sorted(o.Name for o in d.Objects))"
	before := docOutput(t, cs, doc, names)
	fails(t, pdCreate(t, cs, doc, "Missing", "Draft::ShapeString", map[string]any{"String": "X", "FontFile": "C:/no/such/font.ttf"}, nil),
		"C:/no/such/font.ttf", "Nothing was created.")
	if got := docOutput(t, cs, doc, names); got != before {
		t.Fatalf("a refused ShapeString changed the objects: %q, was %q", got, before)
	}
}

// TestDraftArraysAreSolidsAndWorkInBooleans: an OrthoArray of a cylinder 10 x 1 with IntervalX 5 has 10 solids spaced 5; a PolarArray makes 6
// around Z; a Part::Cut of a box by the OrthoArray removes 10 holes.
func TestDraftArraysAreSolidsAndWorkInBooleans(t *testing.T) {
	cs := session(t)
	const doc = "E2EDraftArrays"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Pin", "Part::Cylinder", map[string]any{"Radius": 1, "Height": 5, "Placement": vec(5, 10, 0)}, nil))
	r := pdCreate(t, cs, doc, "Row", "Draft::OrthoArray", map[string]any{"Base": "Pin", "NumberX": 10, "IntervalX": 5}, nil)
	must(t, r, "Shape: 10 solids")
	row := frontValue(r.text, "object_name")
	if strings.Contains(r.text, "flat profile") {
		t.Fatalf("an array is called a flat profile:\n%s", r.text)
	}
	if got := docOutput(t, cs, doc, "print(len(d.getObject('"+row+"').Shape.Solids), sorted(round(s.CenterOfMass.x, 3) + 0.0 for s in d.getObject('"+row+"').Shape.Solids))"); got !=
		"10 [5.0, 10.0, 15.0, 20.0, 25.0, 30.0, 35.0, 40.0, 45.0, 50.0]" {
		t.Fatalf("the row of pins: %q", got)
	}

	r = pdCreate(t, cs, doc, "Ring", "Draft::PolarArray", map[string]any{"Base": "Pin", "NumberPolar": 6, "Angle": 360}, nil)
	must(t, r, "Shape: 6 solids")

	must(t, pdCreate(t, cs, doc, "Plate", "Part::Box", map[string]any{"Length": 60, "Width": 20, "Height": 5}, nil))
	r = pdCreate(t, cs, doc, "Drilled", "Part::Cut", map[string]any{"Base": "Plate", "Tool": row}, nil)
	must(t, r, "Shape: 1 solid")
	if v, want := replyVolume(t, r), 60*20*5-10*math.Pi*1*1*5; math.Abs(v-want) > 1 {
		t.Fatalf("cut volume = %g, want %g (ten holes)", v, want)
	}
	if strings.Contains(r.text, "Warning:") {
		t.Fatalf("the cut by the array warns:\n%s", r.text)
	}
}
