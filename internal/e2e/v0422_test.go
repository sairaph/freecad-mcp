//go:build e2e

package e2e

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func vec(x, y, z float64) map[string]any {
	return map[string]any{"Base": map[string]any{"x": x, "y": y, "z": z}}
}

// pushInto puts the objects into an App::Part with update_object on its Group, the one path the guide names.
func pushInto(t *testing.T, cs *mcp.ClientSession, doc, container string, members ...any) {
	t.Helper()
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": container, "include_screenshot": false,
		"obj_properties": map[string]any{"Group": members}}))
}

// shapeBoxes reads the Shape entry of a get_object reply: its BoundBox and, when there is one, its LocalBoundBox.
func shapeBoxes(t *testing.T, text string) (box, local []float64) {
	t.Helper()
	m := regexp.MustCompile("(?s)~~~json\n(.*)\n~~~").FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("no JSON block in the reply:\n%s", text)
	}
	var obj struct {
		Shape struct {
			BoundBox      []float64
			LocalBoundBox []float64
		}
	}
	if err := json.Unmarshal([]byte(m[1]), &obj); err != nil {
		t.Fatal(err)
	}
	return obj.Shape.BoundBox, obj.Shape.LocalBoundBox
}

func sameNumbers(got, want []float64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-6 {
			return false
		}
	}
	return true
}

// TestContainerObjectsAreReportedGlobally: a 40 x 30 x 3 box in an App::Part placed at (20, 20, 5) sits at x 20..60,
// y 20..50, z 5..8; get_object, list_subelements, measure and check_printability all say so, and check_printability
// says the part floats (a warning, printable stays true) and gives the z range of every part.
func TestContainerObjectsAreReportedGlobally(t *testing.T) {
	cs := session(t)
	const doc = "E2EContainerGlobal"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "PartA", "App::Part", map[string]any{"Placement": vec(20, 20, 5)}, nil))
	must(t, pdCreate(t, cs, doc, "Leaf", "Part::Box", map[string]any{"Length": 40, "Width": 30, "Height": 3}, nil))
	pushInto(t, cs, doc, "PartA", "Leaf")
	must(t, pdCreate(t, cs, doc, "Ref", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 1, "Placement": vec(100, 0, 0)}, nil))
	must(t, pdCreate(t, cs, doc, "Flat", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 3, "Placement": vec(100, 100, 0)}, nil))

	r := call(t, cs, "get_object", map[string]any{"doc_name": doc, "obj_name": "Leaf"})
	must(t, r, "inside PartA, which moves it")
	box, local := shapeBoxes(t, r.text)
	if !sameNumbers(box, []float64{20, 20, 5, 60, 50, 8}) {
		t.Fatalf("global BoundBox = %v, want x 20..60, y 20..50, z 5..8", box)
	}
	if !sameNumbers(local, []float64{0, 0, 0, 40, 30, 3}) {
		t.Fatalf("LocalBoundBox = %v", local)
	}
	// An object in no container has no note, and the container itself reports its own box.
	if r := call(t, cs, "get_object", map[string]any{"doc_name": doc, "obj_name": "Flat"}); strings.Contains(r.text, "LocalBoundBox") {
		t.Fatalf("an object in no container has a local box:\n%s", r.text)
	}

	must(t, call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": "Leaf", "kind": "faces"}),
		"center (40, 35, 5), normal (0, 0, -1), on the bottom", "center (40, 35, 8)")
	// Ref spans x 100..110, y 0..10, z 0..1 and the box x 20..60, y 20..50, z 5..8: the closest points are global.
	must(t, call(t, cs, "measure", map[string]any{"doc_name": doc, "kind": "distance", "refs": []any{
		map[string]any{"object": "Leaf", "sub": "Face2"}, map[string]any{"object": "Ref", "sub": "Face1"}}}), "41.42 mm", "Closest points: [60 20 5] to [100 10 1]")

	r = call(t, cs, "check_printability", map[string]any{"doc_name": doc, "bed_x": 220, "bed_y": 220, "bed_z": 250})
	must(t, r, "printable: true", "PartA: inside the plate, no overlap", "Warning: floats 5 mm above the plate: it needs slicer supports, or move it down so its lowest point is at z 0.",
		"z 5 to 8", "Flat: inside the plate, no overlap", "z 0 to 3")
	// Moving the container down puts it on the plate.
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "PartA", "include_screenshot": false,
		"obj_properties": map[string]any{"Placement": vec(20, 20, 0)}}))
	r = call(t, cs, "check_printability", map[string]any{"doc_name": doc, "bed_x": 220, "bed_y": 220, "bed_z": 250})
	must(t, r, "printable: true", "PartA: inside the plate, no overlap", "z 0 to 3")
	if strings.Contains(r.text, "floats") {
		t.Fatalf("a part on the plate floats:\n%s", r.text)
	}
}

// TestCompactListLeavesOutOriginMembers: an App::Part's and a Body's Origin rows stay, their axes, planes and point go,
// the note says so once, and the full list still has them.
func TestCompactListLeavesOutOriginMembers(t *testing.T) {
	cs := session(t)
	const doc = "E2EOriginRows"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "PartA", "App::Part", nil, nil))
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "Leaf", "Part::Box", nil, nil))
	pushInto(t, cs, doc, "PartA", "Leaf")

	r := call(t, cs, "list_objects", map[string]any{"doc_name": doc, "compact": true})
	must(t, r, "| PartA |", "| Body |", "| Leaf |", "| Origin |",
		"Origin axes and planes are left out: 7 per Origin; list_objects with compact false lists them.")
	for _, gone := range []string{"X_Axis", "Y_Axis", "XY_Plane", "YZ_Plane"} {
		if strings.Contains(r.text, "| "+gone+" |") {
			t.Fatalf("compact list has %s:\n%s", gone, r.text)
		}
	}
	if n := strings.Count(r.text, "Origin axes and planes are left out"); n != 1 {
		t.Fatalf("the note appears %d times:\n%s", n, r.text)
	}
	full := call(t, cs, "list_objects", map[string]any{"doc_name": doc})
	must(t, full, "\"X_Axis\"", "\"XY_Plane\"")
	// 3 containers' worth of rows go: PartA's 7 and Body's 7.
	if short, long := frontCount(t, r, "count"), frontCount(t, full, "count"); long-short != 14 {
		t.Fatalf("the compact list has %d rows, the full one %d, want 14 fewer:\n%s", short, long, r.text)
	}
}

// TestGroupPutsObjectsInAPart: update_object and create_object with Group on an App::Part put objects in it; the list is
// replaced; an object already in another Part is refused by FreeCAD.
func TestGroupPutsObjectsInAPart(t *testing.T) {
	cs := session(t)
	const doc = "E2EPartGroup"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Leaf", "Part::Box", nil, nil))
	must(t, pdCreate(t, cs, doc, "Pin", "Part::Cylinder", nil, nil))
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "PartA", "App::Part", map[string]any{"Group": []any{"Leaf", "Body"}}, nil))
	v0416Code(t, cs, doc, "print([o.Name for o in d.PartA.Group], d.Leaf.getParentGeoFeatureGroup().Name, d.Body.getParentGeoFeatureGroup().Name)",
		"['Leaf', 'Body'] PartA PartA")
	fails(t, pdCreate(t, cs, doc, "PartB", "App::Part", map[string]any{"Group": []any{"Leaf"}}, nil),
		"Object can only be in a single GeoFeatureGroup", "Nothing was created")
	// Moving Leaf to PartB: leave PartA first, then join PartB.
	pushInto(t, cs, doc, "PartA", "Body")
	must(t, pdCreate(t, cs, doc, "PartB", "App::Part", map[string]any{"Group": []any{"Leaf", "Pin"}}, nil))
	v0416Code(t, cs, doc, "print([o.Name for o in d.PartA.Group], [o.Name for o in d.PartB.Group])", "['Body'] ['Leaf', 'Pin']")
}

// TestDraftProfileIsFlatAndExtrudes: a Draft polygon has no solid, the reply says to extrude it, and Part::Extrusion with
// Base the polygon, DirMode Normal and LengthFwd makes the solid.
func TestDraftProfileIsFlatAndExtrudes(t *testing.T) {
	cs := session(t)
	const doc = "E2EDraftProfile"
	newDoc(t, cs, doc)
	r := pdCreate(t, cs, doc, "Hex", "Draft::Polygon", map[string]any{"FacesNumber": 6, "Radius": 10}, nil)
	must(t, r, "Draft::Polygon is a flat profile: extrude it with Part::Extrusion to make a solid.")
	name := frontValue(r.text, "object_name")
	r = pdCreate(t, cs, doc, "HexSolid", "Part::Extrusion", map[string]any{"Base": name, "DirMode": "Normal", "LengthFwd": 5}, nil)
	must(t, r, "Shape: 1 solid", "volume 1299.0")
	if strings.Contains(r.text, "flat profile") {
		t.Fatalf("the extrusion carries the profile note:\n%s", r.text)
	}
}

// TestSetViewNamesTheDirection: the reply names a standard view and says where the camera is.
func TestSetViewNamesTheDirection(t *testing.T) {
	cs := session(t)
	const doc = "E2EViewWords"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "B", "Part::Box", nil, nil))
	for view, want := range map[string]string{
		"Isometric": "looking along (-0.58, 0.58, -0.58): Isometric, from above, from +x and -y.",
		"Front":     "looking along (0, 1, 0): Front, from -y.",
		"Top":       "looking along (0, 0, -1): Top, from above.",
		"Right":     "looking along (-1, 0, 0): Right, from +x.",
		"Bottom":    "looking along (0, 0, 1): Bottom, from below.",
		"Back":      "looking along (0, -1, 0): Back, from +y.",
		"Left":      "looking along (1, 0, 0): Left, from -x.",
	} {
		must(t, call(t, cs, "set_view", map[string]any{"doc_name": doc, "view_name": view, "include_screenshot": false}), want)
	}
}

// TestBodyInAMovedPartIsReportedGlobally: a Pad in a Body (itself placed 3 mm along x) in an App::Part at (20, 20, 5) is
// at x 23..33, y 20..30, z 5..9 for the Pad and for the Body.
func TestBodyInAMovedPartIsReportedGlobally(t *testing.T) {
	cs := session(t)
	const doc = "E2EBodyInPart"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", map[string]any{"Placement": vec(3, 0, 0)}, nil))
	must(t, pdCreate(t, cs, doc, "Sk", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane",
		"Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{0, 0}, "size": []any{10, 10}}}}}, nil))
	must(t, pdCreate(t, cs, doc, "Pad", "PartDesign::Pad", map[string]any{"Profile": "Sk", "Length": 4}, nil))
	must(t, pdCreate(t, cs, doc, "PartA", "App::Part", map[string]any{"Placement": vec(20, 20, 5), "Group": []any{"Body"}}, nil))
	for _, name := range []string{"Pad", "Body"} {
		r := call(t, cs, "get_object", map[string]any{"doc_name": doc, "obj_name": name})
		must(t, r, "which moves it")
		box, _ := shapeBoxes(t, r.text)
		if !sameNumbers(box, []float64{23, 20, 5, 33, 30, 9}) {
			t.Fatalf("%s BoundBox = %v, want x 23..33, y 20..30, z 5..9", name, box)
		}
	}
}

// TestAssemblyWorkflowOfTheGuide: two App::Parts posed in an assembly, the clearance measured between them, the print
// layout made in a copy of the document (the assembly document keeps its pose), checked and exported once for each Part.
func TestAssemblyWorkflowOfTheGuide(t *testing.T) {
	cs := session(t)
	const doc = "E2EAssembly"
	copyPath := tempPath(t, "E2EAssemblyPrint.FCStd")
	folder := filepath.Dir(tempPath(t, "each.step"))
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Leaf", "Part::Box", map[string]any{"Length": 40, "Width": 30, "Height": 3}, nil))
	must(t, pdCreate(t, cs, doc, "Pin", "Part::Cylinder", map[string]any{"Radius": 3, "Height": 10}, nil))
	must(t, pdCreate(t, cs, doc, "PartA", "App::Part", map[string]any{"Group": []any{"Leaf"}}, nil))
	must(t, pdCreate(t, cs, doc, "PartB", "App::Part", map[string]any{"Group": []any{"Pin"}, "Placement": vec(50, 10, 3)}, nil))
	// The Pin's axis stands at x 50, so its side is 7 mm from the Leaf's end (x 40); the Parts' closest points are global.
	must(t, call(t, cs, "measure", map[string]any{"doc_name": doc, "kind": "distance", "refs": []any{
		map[string]any{"object": "PartA"}, map[string]any{"object": "PartB"}}}), "distance of PartA and PartB: 7 mm")

	must(t, call(t, cs, "save_document_as", map[string]any{"doc_name": doc, "path": copyPath, "copy": true}))
	must(t, call(t, cs, "open_document", map[string]any{"path": copyPath}))
	const printDoc = "E2EAssemblyPrint"
	closeOnCleanup(t, cs, printDoc)
	// Lay out in the copy: PartB down on the plate, beside PartA.
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": printDoc, "obj_name": "PartB", "include_screenshot": false,
		"obj_properties": map[string]any{"Placement": vec(60, 10, 0)}}))
	must(t, call(t, cs, "check_printability", map[string]any{"doc_name": printDoc, "bed_x": 220, "bed_y": 220}),
		"printable: true", "PartA: inside the plate, no overlap", "PartB: inside the plate, no overlap")
	// The assembly document keeps its pose and its Pin Part at z 3 floats.
	must(t, call(t, cs, "check_printability", map[string]any{"doc_name": doc, "bed_x": 220, "bed_y": 220}),
		"printable: true", "PartB: inside the plate, no overlap", "Warning: floats 3 mm above the plate: it needs slicer supports")
	must(t, call(t, cs, "export_document", map[string]any{"doc_name": printDoc, "path": folder, "per_object": true,
		"format": "step", "object_names": []string{"PartA", "PartB"}}))
	entries, err := os.ReadDir(folder)
	if err != nil || len(entries) != 2 {
		t.Fatalf("folder %s holds %v, %v; want one file for each Part", folder, entries, err)
	}
}

// exportRange exports the objects in the format and reads the file back with FreeCAD, as "RANGE" numbers: the box of a
// STEP, IGES or BREP shape (xmin, ymin, zmin, xmax, ymax, zmax), the first position accessor of a glTF (metres, y up),
// the x, y and z extent of a DXF's entities, or the x and y extent of an SVG's paths.
func exportRange(t *testing.T, cs *mcp.ClientSession, doc, ext string, names ...string) []float64 {
	t.Helper()
	p := tempPath(t, "out."+ext)
	must(t, call(t, cs, "export_document", map[string]any{"doc_name": doc, "path": p, "object_names": names}))
	var code string
	switch ext {
	case "step", "iges", "brep":
		code = "b = Part.read(p).BoundBox\nprint('RANGE', b.XMin, b.YMin, b.ZMin, b.XMax, b.YMax, b.ZMax)\n"
	case "gltf":
		code = "import json\nj = json.load(open(p))\na = [a for a in j['accessors'] if 'min' in a][0]\nprint('RANGE', *a['min'], *a['max'])\n"
	case "dxf":
		code = "L = open(p).read().split(chr(10))\nstart = L.index('ENTITIES')\nv = {'10': [], '20': [], '30': []}\n" +
			"for i in range(start, len(L) - 1):\n    if L[i].strip() in v:\n        try:\n            v[L[i].strip()].append(float(L[i + 1]))\n        except ValueError:\n            pass\n" +
			"print('RANGE', min(v['10']), min(v['20']), min(v['30']), max(v['10']), max(v['20']), max(v['30']))\n"
	default:
		code = "import re\nn = [(float(x), float(y)) for x, y in re.findall(r'[ML] ([0-9.-]+) ([0-9.-]+)', open(p).read())]\n" +
			"print('RANGE', min(x for x, y in n), min(y for x, y in n), 0, max(x for x, y in n), max(y for x, y in n), 0)\n"
	}
	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "import FreeCAD, Part\np = r'" + p + "'\n" + code})
	m := regexp.MustCompile(`RANGE ((?:[-0-9.e]+ ?){6})`).FindStringSubmatch(r.text)
	if m == nil {
		t.Fatalf("no RANGE in the reply:\n%s", r.text)
	}
	var out []float64
	for _, f := range strings.Fields(m[1]) {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// stepNodes exports the objects to STEP and counts the assembly nodes and products in the file.
func stepNodes(t *testing.T, cs *mcp.ClientSession, doc string, names ...string) (nodes, products int) {
	t.Helper()
	p := tempPath(t, "nodes.step")
	must(t, call(t, cs, "export_document", map[string]any{"doc_name": doc, "path": p, "object_names": names}))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "NEXT_ASSEMBLY_USAGE_OCCURRENCE("), strings.Count(string(data), "PRODUCT(")
}

// TestExportsOfAContainerMemberLandWhereGetObjectSays: Leaf (40 x 30 x 3) and Pin (a cylinder of radius 3 and height 10 at the
// Part's origin) in PartA at (20, 20, 5) are at x 20..60, y 20..50, z 5..8 and x 17..23, y 17..23, z 5..15 in every format, written
// alone, with another object and as the Part; an object alone with a Placement of its own lands there too (a lone object used to be
// written at the origin by STEP, IGES and glTF, and a member or a lone Part at its local place). A Part keeps its assembly nodes.
func TestExportsOfAContainerMemberLandWhereGetObjectSays(t *testing.T) {
	cs := session(t)
	const doc = "E2EExportGlobal"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "PartA", "App::Part", map[string]any{"Placement": vec(20, 20, 5)}, nil))
	must(t, pdCreate(t, cs, doc, "Leaf", "Part::Box", map[string]any{"Length": 40, "Width": 30, "Height": 3}, nil))
	must(t, pdCreate(t, cs, doc, "Pin", "Part::Cylinder", map[string]any{"Radius": 3, "Height": 10}, nil))
	must(t, pdCreate(t, cs, doc, "Far", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 3, "Placement": vec(100, 100, 0)}, nil))
	pushInto(t, cs, doc, "PartA", "Leaf", "Pin")
	leaf := []float64{20, 20, 5, 60, 50, 8}
	part := []float64{17, 17, 5, 60, 50, 15}
	far := []float64{100, 100, 0, 110, 110, 3}
	both := []float64{20, 20, 0, 110, 110, 8}
	partAndFar := []float64{17, 17, 0, 110, 110, 15}
	check := func(what string, got, want []float64) {
		t.Helper()
		for i := range want {
			if math.Abs(got[i]-want[i]) > 1e-4 {
				t.Fatalf("%s = %v, want %v", what, got, want)
			}
		}
	}
	for _, ext := range []string{"step", "iges", "brep"} {
		check(ext+" Leaf", exportRange(t, cs, doc, ext, "Leaf"), leaf)
		check(ext+" Leaf and Far", exportRange(t, cs, doc, ext, "Leaf", "Far"), both)
		check(ext+" PartA", exportRange(t, cs, doc, ext, "PartA"), part)
		check(ext+" PartA and Far", exportRange(t, cs, doc, ext, "PartA", "Far"), partAndFar)
		check(ext+" Far alone", exportRange(t, cs, doc, ext, "Far"), far)
	}
	// The Part keeps its structure, alone (written from a copy) and with another object (written natively): the Part and its two
	// members are three products and two nodes, a plain object is one product.
	if n, p := stepNodes(t, cs, doc, "PartA"); n != 2 || p != 3 {
		t.Fatalf("PartA alone: %d nodes, %d products, want 2 and 3", n, p)
	}
	if n, p := stepNodes(t, cs, doc, "PartA", "Far"); n != 4 || p != 5 {
		t.Fatalf("PartA and Far: %d nodes, %d products, want 4 and 5", n, p)
	}
	if n, p := stepNodes(t, cs, doc, "Far"); n != 0 || p != 1 {
		t.Fatalf("Far alone: %d nodes, %d products", n, p)
	}
	// glTF is in metres, y up (x, z, -y), and the first accessor holds only part of a box: compare it as a range.
	g := exportRange(t, cs, doc, "gltf", "Leaf")
	if g[0] < 0.02-1e-6 || g[3] > 0.06+1e-6 || g[1] < 0.005-1e-6 || g[4] > 0.008+1e-6 || g[2] < -0.05-1e-6 || g[5] > -0.02+1e-6 {
		t.Fatalf("glTF Leaf accessor = %v, want inside x 0.02..0.06, y 0.005..0.008, z -0.05..-0.02", g)
	}
	if far := exportRange(t, cs, doc, "gltf", "Far"); far[0] < 0.1-1e-6 || far[2] > -0.1+1e-6 {
		t.Fatalf("glTF of a lone object with a Placement is at %v, want x from 0.1 and z up to -0.1", far)
	}
	for _, ext := range []string{"dxf", "svg"} {
		check(ext+" Leaf", exportRange(t, cs, doc, ext, "Leaf")[:2], leaf[:2])
		// (a circle is written by its centre, so only the far corner is compared)
		check(ext+" PartA", exportRange(t, cs, doc, ext, "PartA")[3:5], part[3:5])
		check(ext+" Leaf and Far", exportRange(t, cs, doc, ext, "Leaf", "Far")[3:5], both[3:5])
		check(ext+" PartA and Far", exportRange(t, cs, doc, ext, "PartA", "Far")[3:5], partAndFar[3:5])
		check(ext+" Far alone", exportRange(t, cs, doc, ext, "Far")[:2], far[:2])
	}
	// The default set (PartA, Far) was right before and is written natively.
	p := tempPath(t, "default.step")
	must(t, call(t, cs, "export_document", map[string]any{"doc_name": doc, "path": p}))
	code := "import Part\nb = Part.read(r'" + p + "').BoundBox\nprint('RANGE', b.XMin, b.YMin, b.ZMin, b.XMax, b.YMax, b.ZMax)\n"
	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": code})
	must(t, r, "RANGE 17.0 17.0 0.0 110.0 110.0 15.0")
}

// TestChangesInsideAPartAreReported: a sheet cell that rebuilds a Body and a box inside an App::Part (turned 90 degrees
// about x, so the global size differs from the local one) lists both, in global size with the local size beside it.
func TestChangesInsideAPartAreReported(t *testing.T) {
	cs := session(t)
	const doc = "E2EPartChanges"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Params", "Spreadsheet::Sheet", nil, nil))
	must(t, call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{
		map[string]any{"cell": "A1", "content": "4", "alias": "thick"}}}))
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "Sk", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane",
		"Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{0, 0}, "size": []any{10, 10}}}}}, nil))
	must(t, pdCreate(t, cs, doc, "Pad", "PartDesign::Pad", map[string]any{"Profile": "Sk", "Length": "=Params.thick"}, nil))
	must(t, pdCreate(t, cs, doc, "Leaf", "Part::Box", map[string]any{"Length": 20, "Width": 20, "Height": "=Params.thick",
		"Placement": vec(30, 0, 0)}, nil))
	turned := map[string]any{"Base": map[string]any{"x": 20, "y": 20, "z": 5},
		"Rotation": map[string]any{"Axis": map[string]any{"x": 1, "y": 0, "z": 0}, "Angle": 90}}
	must(t, pdCreate(t, cs, doc, "PartA", "App::Part", map[string]any{"Placement": turned, "Group": []any{"Body", "Leaf"}}, nil))

	r := call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{
		map[string]any{"cell": "A1", "content": "6"}}})
	must(t, r, "Body: 1 solid, 10 x 6 x 10 mm (global, inside PartA, which moves it; local 10 x 10 x 6 mm), volume 600.0 mm^3",
		"Leaf: 1 solid, 20 x 6 x 20 mm (global, inside PartA, which moves it; local 20 x 20 x 6 mm), volume 2400.0 mm^3")
	if strings.Contains(r.text, "Pad:") || strings.Contains(r.text, "Sk:") {
		t.Fatalf("a feature inside the Body is listed:\n%s", r.text)
	}
	// The Shape line of update_object says the same.
	r = call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Leaf", "include_screenshot": false,
		"obj_properties": map[string]any{"Length": 30}})
	must(t, r, "Shape: 1 solid, 30 x 6 x 20 mm (global, inside PartA, which moves it; local 30 x 20 x 6 mm)")
}

// TestFullListLeavesOutTheIntegrals: the full object list has the counts and the box of each shape but no Volume, Area or
// CenterOfMass, and says so once; get_object still gives them. 100 threaded bolts in one compound cost 15.5 s in the list before.
func TestFullListLeavesOutTheIntegrals(t *testing.T) {
	cs := session(t)
	const doc = "E2EListIntegrals"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Box", "Part::Box", map[string]any{"Length": 10, "Width": 5, "Height": 2}, nil))
	r := call(t, cs, "list_objects", map[string]any{"doc_name": doc})
	must(t, r, "\"SolidCount\": 1", "\"BoundBox\"", "Volume, area and centre of mass are left out of this list: get_object gives them for one object.")
	for _, gone := range []string{"\"Volume\"", "\"Area\"", "\"CenterOfMass\""} {
		if strings.Contains(r.text, gone) {
			t.Fatalf("the full list has %s:\n%s", gone, r.text)
		}
	}
	if n := strings.Count(r.text, "left out of this list"); n != 1 {
		t.Fatalf("the note appears %d times", n)
	}
	must(t, call(t, cs, "get_object", map[string]any{"doc_name": doc, "obj_name": "Box"}), "\"Volume\":", "\"Area\":", "\"CenterOfMass\":")
	if r := call(t, cs, "list_objects", map[string]any{"doc_name": doc, "compact": true}); strings.Contains(r.text, "left out of this list") {
		t.Fatalf("the compact list carries the integrals note:\n%s", r.text)
	}

	// 100 threaded bolts in one compound: the list stays fast.
	const bolts = "import time\npitch, major, length = 0.7, 4.0, 12.0\nr = major / 2\nroot = r - 0.43\n" +
		"helix = Part.makeHelix(pitch, length, root)\n" +
		"prof = Part.makePolygon([FreeCAD.Vector(root - 0.15, 0, -0.45 * pitch), FreeCAD.Vector(r, 0, -0.08 * pitch), FreeCAD.Vector(r, 0, 0.08 * pitch), FreeCAD.Vector(root - 0.15, 0, 0.45 * pitch), FreeCAD.Vector(root - 0.15, 0, -0.45 * pitch)])\n" +
		"thread = Part.Wire(helix).makePipeShell([prof], True, True)\n" +
		"core = Part.makeCylinder(root, length + pitch, FreeCAD.Vector(0, 0, -pitch / 2))\n" +
		"shaft = core.fuse(thread).common(Part.makeCylinder(r + 1, length)).removeSplitter()\n" +
		"bolt = shaft.fuse(Part.makeCylinder(3.5, 2.8, FreeCAD.Vector(0, 0, length))).removeSplitter()\n" +
		"solids = []\nfor i in range(100):\n    b = bolt.copy()\n    b.translate(FreeCAD.Vector((i % 10) * 10 + 20, (i // 10) * 10 + 20, 0))\n    solids.append(b)\n" +
		"o = d.addObject('Part::Feature', 'Bolts')\no.Shape = Part.makeCompound(solids)\nd.recompute()\nprint('BOLTS', len(o.Shape.Solids))\n"
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "timeout": 300,
		"code": "import FreeCAD, Part\nd = FreeCAD.getDocument('" + doc + "')\n" + bolts}), "BOLTS 100")
	start := time.Now()
	r = call(t, cs, "list_objects", map[string]any{"doc_name": doc})
	took := time.Since(start)
	must(t, r, "\"SolidCount\": 100")
	t.Logf("list_objects of 100 threaded solids: %.2f s", took.Seconds())
	if took > 5*time.Second {
		t.Fatalf("the full list took %v for 100 solids", took)
	}
}

// openDocuments reads the open documents and the active one from FreeCAD.
func openDocuments(t *testing.T, cs *mcp.ClientSession) string {
	t.Helper()
	r := call(t, cs, "execute_code", map[string]any{"include_screenshot": false,
		"code": "print('DOCS', sorted(FreeCAD.listDocuments()), FreeCAD.ActiveDocument.Name if FreeCAD.ActiveDocument else None)"})
	i := strings.Index(r.text, "DOCS")
	if i < 0 {
		t.Fatalf("no DOCS in the reply:\n%s", r.text)
	}
	return strings.TrimSpace(strings.SplitN(r.text[i:], "\n", 2)[0])
}

// TestExportCopiesLeaveNoDocumentBehind: the hidden temporary document an export of a container member uses is closed after
// STEP, glTF, DXF and SVG, and after an export that fails inside it (the target is a folder), and the active document is the one it was.
func TestExportCopiesLeaveNoDocumentBehind(t *testing.T) {
	cs := session(t)
	const doc, other = "E2ECopyDocs", "E2ECopyDocsOther"
	newDoc(t, cs, other)
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "PartA", "App::Part", map[string]any{"Placement": vec(20, 20, 5)}, nil))
	must(t, pdCreate(t, cs, doc, "Leaf", "Part::Box", nil, nil))
	pushInto(t, cs, doc, "PartA", "Leaf")
	must(t, call(t, cs, "activate_document", map[string]any{"doc_name": other}))
	before := openDocuments(t, cs)
	if strings.Contains(before, "MCPExport") || !strings.HasSuffix(before, "] "+other) {
		t.Fatalf("starting state = %s", before)
	}
	for _, ext := range []string{"step", "gltf", "dxf", "svg"} {
		must(t, call(t, cs, "export_document", map[string]any{"doc_name": doc, "path": tempPath(t, "c."+ext), "object_names": []string{"Leaf"}}))
		if after := openDocuments(t, cs); after != before {
			t.Fatalf("after the %s export: %s, before: %s", ext, after, before)
		}
	}
	// A lone Part takes the Part-copy route.
	must(t, call(t, cs, "export_document", map[string]any{"doc_name": doc, "path": tempPath(t, "p.step"), "object_names": []string{"PartA"}}))
	if after := openDocuments(t, cs); after != before {
		t.Fatalf("after the Part export: %s, before: %s", after, before)
	}
	// A failing export inside the copy: the target is a folder that has the file's name.
	blocked := tempPath(t, "blocked.step")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false,
		"code": "import os\nos.makedirs(r'" + blocked + "')\nprint('MADE')"}), "MADE")
	r := call(t, cs, "export_document", map[string]any{"doc_name": doc, "path": blocked, "overwrite": true, "object_names": []string{"Leaf"}})
	if !r.isErr {
		t.Fatalf("an export onto a folder did not fail:\n%s", r.text)
	}
	if after := openDocuments(t, cs); after != before {
		t.Fatalf("after the failing export: %s, before: %s", after, before)
	}
}
