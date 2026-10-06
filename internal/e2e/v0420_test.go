//go:build e2e

package e2e

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestImportNamesTheFile: the reply says which file came in, not its extension.
func TestImportNamesTheFile(t *testing.T) {
	cs := session(t)
	const doc = "E2EImportName"
	newDoc(t, cs, doc)
	pdCreate(t, cs, doc, "Block", "Part::Box", map[string]any{"Length": 12, "Width": 8, "Height": 5}, nil)
	path := tempPath(t, "verify10.step")
	must(t, call(t, cs, "export_document", map[string]any{"doc_name": doc, "path": path}))
	r := call(t, cs, "import_file", map[string]any{"path": path, "include_screenshot": false})
	must(t, r, "Imported 'verify10.step' into new document")
	if strings.Contains(r.text, "'.step'") {
		t.Fatalf("reply still names the extension:\n%s", r.text)
	}
	closeOnCleanup(t, cs, frontValue(r.text, "document"))
	// The STEP round trip keeps the volume (read from the Shape line of each).
	original := call(t, cs, "measure", map[string]any{"doc_name": doc, "kind": "volume", "refs": []any{map[string]any{"object": "Block"}}})
	imported := call(t, cs, "measure", map[string]any{"doc_name": frontValue(r.text, "document"), "kind": "volume", "refs": []any{map[string]any{"object": firstCreated(t, r.text)}}})
	if a, b := frontFloat(t, original, "value"), frontFloat(t, imported, "value"); math.Abs(a-b) > 1e-6 || math.Abs(a-480) > 1e-6 {
		t.Fatalf("volume %v became %v", a, b)
	}
}

func firstCreated(t *testing.T, text string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^- (\S+) \(label`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("no created object listed:\n%s", text)
	}
	return m[1]
}

// shapesLine returns the "Shapes now" text of a reply ("" when it has none).
func shapesLine(text string) string {
	if i := strings.Index(text, "Shapes now:"); i >= 0 {
		rest := text[i:]
		if j := strings.Index(rest, "\n\n"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	return ""
}

// sheetPlate builds a PartDesign plate from an AdditiveBox and two SubtractiveCylinders, every size and position bound to a sheet.
func sheetPlate(t *testing.T, cs *mcp.ClientSession, doc string) {
	t.Helper()
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Params", "Spreadsheet::Sheet", nil, nil))
	cells := []any{}
	for i, kv := range [][2]string{{"platelength", "60"}, {"platewidth", "40"}, {"thick", "5"}, {"holeradius", "1.6"}, {"holex", "10"}, {"holexb", "41"}, {"holey", "20"}} {
		cells = append(cells, map[string]any{"cell": "A" + strconv.Itoa(i+1), "content": kv[1], "alias": kv[0]})
	}
	must(t, call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": cells}))
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "Plate", "PartDesign::AdditiveBox", map[string]any{"Length": "=Params.platelength", "Width": "=Params.platewidth", "Height": "=Params.thick"}, nil), "In Body 'Body'")
	for name, x := range map[string]string{"Hole1": "=Params.holex", "Hole2": "=Params.holexb"} {
		must(t, pdCreate(t, cs, doc, name, "PartDesign::SubtractiveCylinder", map[string]any{"Radius": "=Params.holeradius", "Height": "=Params.thick", "Placement.Base.x": x, "Placement.Base.y": "=Params.holey"}, nil), "In Body 'Body'")
	}
}

// TestSheetChangeUndoRedoAndDeleteReportTheShapes: a PartDesign plate driven by a sheet; the sheet change, undo and redo each say the
// Body's new size; a recompute with nothing to do and the deletion of an unrelated object say nothing.
func TestSheetChangeUndoRedoAndDeleteReportTheShapes(t *testing.T) {
	cs := session(t)
	const doc = "E2EShapes"
	sheetPlate(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Loose", "Part::Box", map[string]any{"Length": 3, "Width": 3, "Height": 3, "Placement": map[string]any{"Base": map[string]any{"x": 200, "y": 0, "z": 0}}}, nil))
	volume := func(width float64) float64 { return 60*width*5 - 2*math.Pi*1.6*1.6*5 }

	r := call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{map[string]any{"cell": "A2", "content": "56"}}})
	line := shapesLine(r.text)
	if !strings.Contains(line, "Shapes now: Body: 1 solid, 60 x 56 x 5 mm, volume "+strconv.FormatFloat(math.Round(volume(56)*10)/10, 'f', 1, 64)) {
		t.Fatalf("sheet change line = %q\n%s", line, r.text)
	}
	if strings.Contains(r.text, "Plate:") || strings.Contains(r.text, "Hole") {
		t.Fatalf("features inside the Body are listed:\n%s", r.text)
	}
	r = call(t, cs, "undo", map[string]any{"doc_name": doc, "include_screenshot": false})
	if line = shapesLine(r.text); !strings.Contains(line, "Body: 1 solid, 60 x 40 x 5 mm") {
		t.Fatalf("undo line = %q\n%s", line, r.text)
	}
	r = call(t, cs, "redo", map[string]any{"doc_name": doc, "include_screenshot": false})
	if line = shapesLine(r.text); !strings.Contains(line, "Body: 1 solid, 60 x 56 x 5 mm") {
		t.Fatalf("redo line = %q\n%s", line, r.text)
	}
	// Nothing to recompute, and an unrelated object deleted: no shape line.
	r = call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false})
	if shapesLine(r.text) != "" {
		t.Fatalf("a no-op recompute reports shapes:\n%s", r.text)
	}
	r = call(t, cs, "delete_object", map[string]any{"doc_name": doc, "obj_name": "Loose", "include_screenshot": false})
	if shapesLine(r.text) != "" {
		t.Fatalf("deleting an unrelated object reports shapes:\n%s", r.text)
	}
	// Deleting a feature changes the Body.
	r = call(t, cs, "delete_object", map[string]any{"doc_name": doc, "obj_name": "Hole2", "include_screenshot": false})
	if line = shapesLine(r.text); !strings.Contains(line, "Body: 1 solid, 60 x 56 x 5 mm") {
		t.Fatalf("delete line = %q\n%s", line, r.text)
	}
}

// circleEdgesAtTop returns the circle edges of a list_subelements edge table at the highest z, left to right.
func circleEdgesAtTop(t *testing.T, table string) []string {
	t.Helper()
	row := regexp.MustCompile(`\| (Edge\d+) \| circle \|.*center \(([-0-9.]+), ([-0-9.]+), ([-0-9.]+)\)`)
	type edge struct {
		name string
		x, z float64
	}
	var all []edge
	top := math.Inf(-1)
	for _, m := range row.FindAllStringSubmatch(table, -1) {
		x, _ := strconv.ParseFloat(m[2], 64)
		z, _ := strconv.ParseFloat(m[4], 64)
		all = append(all, edge{m[1], x, z})
		top = math.Max(top, z)
	}
	var names []string
	for pass := 0; pass < len(all); pass++ {
		best := -1
		for i, e := range all {
			if math.Abs(e.z-top) < 1e-6 && e.name != "" && (best < 0 || e.x < all[best].x) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		names = append(names, all[best].name)
		all[best].name = ""
	}
	return names
}

// TestMeasureCenterGivesTheHoleSpacing: two holes 31 mm apart measure 31.000 from centre to centre and from axis to axis, where the
// edges themselves are 27.8 mm apart rim to rim.
func TestMeasureCenterGivesTheHoleSpacing(t *testing.T) {
	cs := session(t)
	const doc = "E2EMeasureCenter"
	sheetPlate(t, cs, doc)
	table := call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": "Body", "kind": "edges"}).text
	holes := circleEdgesAtTop(t, table)
	if len(holes) != 2 {
		t.Fatalf("hole edges = %v\n%s", holes, table)
	}
	dist := func(a, b map[string]any) reply {
		return call(t, cs, "measure", map[string]any{"doc_name": doc, "kind": "distance", "refs": []any{a, b}})
	}
	ref := func(sub string, point bool) map[string]any {
		m := map[string]any{"object": "Body", "sub": sub}
		if point {
			m["point"] = "center"
		}
		return m
	}
	rim := dist(ref(holes[0], false), ref(holes[1], false))
	if v := frontFloat(t, rim, "value"); math.Abs(v-27.8) > 1e-6 {
		t.Fatalf("rim to rim = %v, want 27.8", v)
	}
	centre := dist(ref(holes[0], true), ref(holes[1], true))
	must(t, centre, "Measured between the centre of Body."+holes[0], "circle of radius 1.6")
	if v := frontFloat(t, centre, "value"); math.Abs(v-31) > 1e-9 {
		t.Fatalf("centre to centre = %v, want 31", v)
	}
	// The cylinder faces of the two holes: axis to axis.
	faces := call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": "Body", "kind": "faces"}).text
	cyl := regexp.MustCompile(`\| (Face\d+) \| cylinder \|`).FindAllStringSubmatch(faces, -1)
	if len(cyl) != 2 {
		t.Fatalf("cylinder faces = %v\n%s", cyl, faces)
	}
	axes := dist(ref(cyl[0][1], true), ref(cyl[1][1], true))
	must(t, axes, "axis of Body."+cyl[0][1])
	if v := frontFloat(t, axes, "value"); math.Abs(v-31) > 1e-9 {
		t.Fatalf("axis to axis = %v, want 31", v)
	}
	// A centre to an axis is the perpendicular distance.
	mixed := dist(ref(holes[0], true), ref(cyl[1][1], true))
	if v := frontFloat(t, mixed, "value"); math.Abs(v-31) > 1e-9 {
		t.Fatalf("centre to axis = %v, want 31", v)
	}
	// center on something with no centre, and on another kind, is refused.
	flat := regexp.MustCompile(`\| (Face\d+) \| plane \|`).FindStringSubmatch(faces)
	fails(t, dist(ref(flat[1], true), ref(holes[0], true)), "has no centre")
	fails(t, call(t, cs, "measure", map[string]any{"doc_name": doc, "kind": "radius", "refs": []any{ref(holes[0], true)}}), "is for distance")
}

// TestSlotSketchPadsIntoAValidSolid: a slot is a closed profile whose pad has the volume of a stadium prism.
func TestSlotSketchPadsIntoAValidSolid(t *testing.T) {
	cs := session(t)
	const doc = "E2ESlot"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	for _, c := range []struct {
		name         string
		a, b         []any
		width, depth float64
	}{{"Flat", []any{10, 10}, []any{30, 10}, 8, 5}} {
		r := pdCreate(t, cs, doc, c.name, "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane",
			"Geometry": []any{map[string]any{"slot": map[string]any{"center1": c.a, "center2": c.b, "width": c.width}}}}, nil)
		must(t, r, "Sketch: 4 geometry elements, closed profile.")
		r = pdCreate(t, cs, doc, "Pad"+c.name, "PartDesign::Pad", map[string]any{"Profile": c.name, "Length": c.depth}, nil)
		want := c.depth * (20*c.width + math.Pi*c.width*c.width/4)
		if v := replyVolume(t, r); math.Abs(v-want) > 0.1 {
			t.Fatalf("slot pad volume = %g, want %g", v, want)
		}
	}
	// A diagonal slot in a second Body.
	must(t, pdCreate(t, cs, doc, "Other", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "Diag", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane",
		"Geometry": []any{map[string]any{"slot": map[string]any{"center1": []any{0, 0}, "center2": []any{-20, -15}, "width": 6}}}}, map[string]any{"body_name": "Other"}),
		"closed profile")
	r := pdCreate(t, cs, doc, "PadDiag", "PartDesign::Pad", map[string]any{"Profile": "Diag", "Length": 5}, nil)
	if v, want := replyVolume(t, r), 5*(25*6+math.Pi*9); math.Abs(v-want) > 0.1 {
		t.Fatalf("diagonal slot pad volume = %g, want %g", v, want)
	}
	v0416Code(t, cs, doc, "print(d.getObject('PadDiag').Shape.isValid(), d.getObject('PadFlat').Shape.isValid())", "True True")
	for name, spec := range map[string]map[string]any{
		"same centres": {"center1": []any{1, 1}, "center2": []any{1, 1}, "width": 4},
		"no width":     {"center1": []any{0, 0}, "center2": []any{5, 0}, "width": 0},
	} {
		t.Log(name)
		fails(t, pdCreate(t, cs, doc, "Bad", "Sketcher::SketchObject", map[string]any{"Geometry": []any{map[string]any{"slot": spec}}}, nil), "Nothing was created")
	}
}

// TestSheetDrivenPlateFollowsItsCells: the AdditiveBox and SubtractiveCylinders follow the sheet (size, hole spacing), and a sketch
// follows a cell through its AttachmentOffset.
func TestSheetDrivenPlateFollowsItsCells(t *testing.T) {
	cs := session(t)
	const doc = "E2ESheetPlate"
	sheetPlate(t, cs, doc)
	r := call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{
		map[string]any{"cell": "A1", "content": "70"}, map[string]any{"cell": "A6", "content": "47"}}})
	must(t, r, "Body: 1 solid, 70 x 40 x 5 mm")
	table := call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": "Body", "kind": "edges"}).text
	holes := circleEdgesAtTop(t, table)
	d := call(t, cs, "measure", map[string]any{"doc_name": doc, "kind": "distance", "refs": []any{
		map[string]any{"object": "Body", "sub": holes[0], "point": "center"}, map[string]any{"object": "Body", "sub": holes[1], "point": "center"}}})
	if v := frontFloat(t, d, "value"); math.Abs(v-37) > 1e-9 {
		t.Fatalf("hole spacing = %v, want 37", v)
	}
	// A sketch moved by a cell.
	must(t, call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{map[string]any{"cell": "A8", "content": "5", "alias": "dx"}}}))
	must(t, pdCreate(t, cs, doc, "Sk", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane", "AttachmentOffset.Base.x": "=Params.dx",
		"Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{0, 0}, "size": []any{10, 10}}}}}, map[string]any{"body_name": "Body"}))
	must(t, call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{map[string]any{"cell": "A8", "content": "12"}}}))
	v0416Code(t, cs, doc, "b = d.getObject('Sk').Shape.BoundBox\nprint(round(b.XMin, 6), round(b.XMax, 6))", "12.0 22.0")
}

// TestPocketDirection: a Pocket cuts against its sketch's normal; the numbers the guide states.
func TestPocketDirection(t *testing.T) {
	cs := session(t)
	const doc = "E2EPocketDirection"
	newDoc(t, cs, doc)
	rect := func(x float64) []any {
		return []any{map[string]any{"rectangle": map[string]any{"corner": []any{x, x}, "size": []any{10, 10}}}}
	}
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "S0", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane", "Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{0, 0}, "size": []any{40, 40}}}}}, nil))
	r := pdCreate(t, cs, doc, "Pad", "PartDesign::Pad", map[string]any{"Profile": "S0", "Length": 10}, nil)
	if v := replyVolume(t, r); math.Abs(v-16000) > 0.1 {
		t.Fatalf("pad = %g", v)
	}
	must(t, pdCreate(t, cs, doc, "Top", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": []any{"Pad", "Face6"}, "Geometry": rect(5)}, nil))
	r = pdCreate(t, cs, doc, "PocketTop", "PartDesign::Pocket", map[string]any{"Profile": "Top", "Length": 3}, nil)
	if v := replyVolume(t, r); math.Abs(v-15700) > 0.1 {
		t.Fatalf("pocket from the top face = %g, want 15700 (300 removed)", v)
	}
	must(t, pdCreate(t, cs, doc, "Under", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane", "Geometry": rect(25)}, nil))
	r = pdCreate(t, cs, doc, "PocketUnder", "PartDesign::Pocket", map[string]any{"Profile": "Under", "Length": 3}, nil)
	if v := replyVolume(t, r); math.Abs(v-15700) > 0.1 {
		t.Fatalf("pocket from XY_Plane under the part = %g, want 15700 (nothing removed)", v)
	}
	r = call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "PocketUnder", "include_screenshot": false, "obj_properties": map[string]any{"Reversed": true}})
	if v := replyVolume(t, r); math.Abs(v-15400) > 0.1 {
		t.Fatalf("reversed pocket from XY_Plane = %g, want 15400 (300 removed)", v)
	}
}

// TestUndoAndRedoOfADeleteInsideABody: deleting a feature of a Body chain (the middle one, then the Tip) leaves exactly the expected
// Group, Tip, BaseFeatures and volume; undo restores the whole chain and redo removes the feature again the same way.
func TestUndoAndRedoOfADeleteInsideABody(t *testing.T) {
	cs := session(t)
	const doc = "E2EBodyDeleteUndo"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "Plate", "PartDesign::AdditiveBox", map[string]any{"Length": 60, "Width": 40, "Height": 5}, nil))
	for i, name := range []string{"Hole1", "Hole2", "Hole3"} {
		must(t, pdCreate(t, cs, doc, name, "PartDesign::SubtractiveCylinder", map[string]any{"Radius": 2, "Height": 5,
			"Placement": map[string]any{"Base": map[string]any{"x": 10 + 20*i, "y": 20, "z": 0}}}, nil))
	}
	// state reads Group, Tip, every BaseFeature, the volume and the features that are not up to date.
	state := func() string {
		r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "d = FreeCAD.getDocument('" + doc + "')\nb = d.getObject('Body')\n" +
			"print([o.Name for o in b.Group], b.Tip.Name, {o.Name: (o.BaseFeature.Name if o.BaseFeature else None) for o in b.Group}, round(b.Shape.Volume, 3), [o.Name for o in b.Group if o.State != ['Up-to-date']])"})
		_, out, found := strings.Cut(r.text, "Output: ")
		if !found {
			t.Fatalf("execute_code reply has no Output marker:\n%s", r.text)
		}
		return strings.TrimSpace(out)
	}
	const whole = "['Plate', 'Hole1', 'Hole2', 'Hole3'] Hole3 {'Plate': None, 'Hole1': 'Plate', 'Hole2': 'Hole1', 'Hole3': 'Hole2'} 11811.504 []"
	for _, c := range []struct{ victim, deleted string }{
		{"Hole2", "['Plate', 'Hole1', 'Hole3'] Hole3 {'Plate': None, 'Hole1': 'Plate', 'Hole3': 'Hole1'} 11874.336 []"},
		{"Hole3", "['Plate', 'Hole1', 'Hole2'] Hole2 {'Plate': None, 'Hole1': 'Plate', 'Hole2': 'Hole1'} 11874.336 []"},
	} {
		if got := state(); got != whole {
			t.Fatalf("chain before deleting %s:\nwant %s\ngot  %s", c.victim, whole, got)
		}
		must(t, call(t, cs, "delete_object", map[string]any{"doc_name": doc, "obj_name": c.victim, "include_screenshot": false}), "Shapes now: Body: 1 solid, 60 x 40 x 5 mm")
		if got := state(); got != c.deleted {
			t.Fatalf("Body after deleting %s:\nwant %s\ngot  %s", c.victim, c.deleted, got)
		}
		must(t, call(t, cs, "undo", map[string]any{"doc_name": doc, "include_screenshot": false}), "Undone 1 transaction(s)", "Shapes now: Body")
		if got := state(); got != whole {
			t.Fatalf("undo of deleting %s:\nwant %s\ngot  %s", c.victim, whole, got)
		}
		must(t, call(t, cs, "redo", map[string]any{"doc_name": doc, "include_screenshot": false}), "Redone 1 transaction(s)", "Shapes now: Body")
		if got := state(); got != c.deleted {
			t.Fatalf("redo of deleting %s:\nwant %s\ngot  %s", c.victim, c.deleted, got)
		}
		must(t, call(t, cs, "undo", map[string]any{"doc_name": doc, "include_screenshot": false}))
		if got := state(); got != whole {
			t.Fatalf("second undo of deleting %s:\nwant %s\ngot  %s", c.victim, whole, got)
		}
	}
}
