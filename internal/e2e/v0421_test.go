//go:build e2e

package e2e

import (
	"strconv"
	"strings"
	"testing"
)

// TestWrongPropertyNamesGetAHint: a name the object does not have is refused with its nearest properties, and with the type that has
// the name for the Base and Tool or Shapes mix-up of the booleans, in create_object and in update_object.
func TestWrongPropertyNamesGetAHint(t *testing.T) {
	cs := session(t)
	const doc = "E2EPropertyHints"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "A", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 10}, nil))
	fails(t, pdCreate(t, cs, doc, "Common", "Part::Common", map[string]any{"Shapes": []any{"A", "A"}}, nil),
		"Part::Common 'Common' has no property 'Shapes'", "Part::Common takes Base and Tool; Part::MultiCommon and Part::MultiFuse take Shapes.", "Nothing was created")
	fails(t, pdCreate(t, cs, doc, "M", "Part::MultiFuse", map[string]any{"Base": "A", "Tool": "A"}, nil),
		"Part::MultiFuse takes Shapes, a list of objects; Part::Common, Part::Cut and Part::Fuse take Base and Tool.")
	fails(t, pdCreate(t, cs, doc, "B", "Part::Box", map[string]any{"Lenght": 10}, nil),
		"Part::Box 'B' has no property 'Lenght'", "Nearest properties: Length", "Nothing was created")
	fails(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "A", "include_screenshot": false, "obj_properties": map[string]any{"Hieght": 4, "Width": 3}}),
		"has no property 'Hieght'", "Nearest properties: Height", "Nothing was changed")
	// The right forms work.
	must(t, pdCreate(t, cs, doc, "A2", "Part::Box", map[string]any{"Placement": map[string]any{"Base": map[string]any{"x": 5, "y": 0, "z": 0}}}, nil))
	must(t, pdCreate(t, cs, doc, "Both", "Part::MultiFuse", map[string]any{"Shapes": []any{"A", "A2"}}, nil))
	v0416Code(t, cs, doc, "print(d.getObject('A').Width, d.getObject('A').Height)", "10.0 mm 10.0 mm")
}

// TestUpdatingABodysSketchReportsTheBody: the Body a Pad built on the sketch is rebuilt, so its new shape is listed; the sketch's own
// shape is in its own Shape line, and creating the Pad does not list its own Body twice.
func TestUpdatingABodysSketchReportsTheBody(t *testing.T) {
	cs := session(t)
	const doc = "E2ESketchBody"
	newDoc(t, cs, doc)
	rect := func(w, h int) map[string]any {
		return map[string]any{"Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{0, 0}, "size": []any{w, h}}}}}
	}
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	sk := rect(40, 40)
	sk["AttachmentSupport"] = "XY_Plane"
	must(t, pdCreate(t, cs, doc, "Sk", "Sketcher::SketchObject", sk, nil))
	r := pdCreate(t, cs, doc, "Pad", "PartDesign::Pad", map[string]any{"Profile": "Sk", "Length": 4}, nil)
	must(t, r, "Shape: 1 solid, 40 x 40 x 4 mm")
	if strings.Contains(r.text, "Shapes now") {
		t.Fatalf("creating the Pad lists its own Body:\n%s", r.text)
	}
	r = call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Sk", "include_screenshot": false, "obj_properties": rect(60, 40)})
	must(t, r, "Shape: no solid, 4 edges, 60 x 40 x 0 mm", "Shapes now: Body: 1 solid, 60 x 40 x 4 mm, volume 9600.0 mm^3")
	// An update that rebuilds nothing else lists nothing.
	r = call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Pad", "include_screenshot": false, "obj_properties": map[string]any{"Length": 4}})
	if strings.Contains(r.text, "Shapes now") {
		t.Fatalf("an unchanged update lists shapes:\n%s", r.text)
	}
}

func frontCount(t *testing.T, r reply, key string) int {
	t.Helper()
	n, err := strconv.Atoi(frontValue(r.text, key))
	if err != nil {
		t.Fatalf("front matter %s is not a count: %v\n%s", key, err, r.text)
	}
	return n
}

// TestListSubelementsFilters: on a box (along z, on_bottom, curve, surface, min_length) and on a mesh-derived solid, where the filter
// keeps the reply small and the frontmatter keeps the totals.
func TestListSubelementsFilters(t *testing.T) {
	cs := session(t)
	const doc = "E2EFilters"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Box", "Part::Box", map[string]any{"Length": 20, "Width": 10, "Height": 5}, nil))
	list := func(obj string, args map[string]any) reply {
		args["doc_name"], args["obj_name"] = doc, obj
		return call(t, cs, "list_subelements", args)
	}
	r := list("Box", map[string]any{"kind": "edges", "along": "z"})
	if frontCount(t, r, "edges") != 12 || frontCount(t, r, "edges_matched") != 4 || strings.Count(r.text, "| line |") != 4 {
		t.Fatalf("along z:\n%s", r.text)
	}
	r = list("Box", map[string]any{"kind": "all", "on_bottom": true})
	if frontCount(t, r, "faces_matched") != 1 || frontCount(t, r, "edges_matched") != 4 || frontCount(t, r, "faces") != 6 || frontCount(t, r, "edges") != 12 {
		t.Fatalf("on_bottom:\n%s", r.text)
	}
	r = list("Box", map[string]any{"kind": "edges", "min_length": 15})
	if frontCount(t, r, "edges_matched") != 4 { // the four 20 mm edges
		t.Fatalf("min_length:\n%s", r.text)
	}
	r = list("Box", map[string]any{"kind": "faces", "surface": "plane", "on_bottom": false})
	if frontCount(t, r, "faces_matched") != 5 {
		t.Fatalf("surface plane, not on the bottom:\n%s", r.text)
	}
	must(t, list("Box", map[string]any{"kind": "edges", "curve": "circle"}), "No edges match the filters (the shape has 12).")
	r = list("Box", map[string]any{"kind": "edges", "smooth": true})
	if frontCount(t, r, "edges_matched") != 0 {
		t.Fatalf("a box has no smooth edge:\n%s", r.text)
	}
	fails(t, list("Box", map[string]any{"kind": "faces", "along": "z"}), "along filters edges")
	fails(t, list("Box", map[string]any{"kind": "edges", "surface": "plane"}), "surface filters faces")
	// Without filters nothing changes.
	r = list("Box", map[string]any{"kind": "edges"})
	if frontValue(r.text, "edges_matched") != "" || frontCount(t, r, "edges") != 12 {
		t.Fatalf("unfiltered reply changed:\n%s", r.text)
	}

	// A mesh-derived solid: thousands of triangle edges, filtered to the few that matter.
	must(t, pdCreate(t, cs, doc, "Ball", "Part::Sphere", map[string]any{"Radius": 10}, nil))
	must(t, call(t, cs, "solid_to_mesh", map[string]any{"doc_name": doc, "obj_name": "Ball", "include_screenshot": false}))
	conv := call(t, cs, "mesh_to_solid", map[string]any{"doc_name": doc, "obj_name": "Ball_mesh", "refine": true, "include_screenshot": false})
	must(t, conv, "face(s), a closed solid.", "Refine merged only flat regions: curved surfaces keep one face per triangle",
		"The source mesh 'Ball_mesh' stays visible.", "before check_printability or exporting the default set")
	faces := frontCount(t, conv, "faces")
	if faces < 1000 {
		t.Fatalf("a refined sphere of triangles has %d faces, want one per triangle", faces)
	}
	all := list("Ball_mesh_solid", map[string]any{"kind": "edges", "min_length": 1e9})
	total := frontCount(t, all, "edges")
	if total < 3000 || frontCount(t, all, "edges_matched") != 0 || len(all.text) > 2000 {
		t.Fatalf("filtered to nothing: total %d, reply %d bytes", total, len(all.text))
	}
	bottom := list("Ball_mesh_solid", map[string]any{"kind": "edges", "on_bottom": true})
	smooth := list("Ball_mesh_solid", map[string]any{"kind": "edges", "smooth": false, "min_length": 0.5})
	if frontCount(t, bottom, "edges") != total || frontCount(t, bottom, "edges_matched") >= total/2 || frontCount(t, smooth, "edges_matched") >= total {
		t.Fatalf("mesh filters: total %d, on_bottom %d, not smooth %d", total, frontCount(t, bottom, "edges_matched"), frontCount(t, smooth, "edges_matched"))
	}
	t.Logf("mesh-derived solid: %d faces, %d edges; on the bottom %d; not smooth and 0.5 mm or longer %d", faces, total,
		frontCount(t, bottom, "edges_matched"), frontCount(t, smooth, "edges_matched"))
}

// TestRoundingAMeshSolidWithACutTool and the D-shaft hole: the guide's recipes give the volumes it states.
func TestGuideRecipesForCuttingToolsGiveTheirVolumes(t *testing.T) {
	cs := session(t)
	const doc = "E2ECutRecipes"
	newDoc(t, cs, doc)
	base := map[string]any{"x": 100, "y": 0, "z": 0}
	at := map[string]any{"Base": base}
	must(t, pdCreate(t, cs, doc, "Knob", "Part::Cylinder", map[string]any{"Radius": 10, "Height": 10, "Placement": at}, nil))
	must(t, pdCreate(t, cs, doc, "Ring", "Part::Cylinder", map[string]any{"Radius": 12, "Height": 1, "Placement": at}, nil))
	must(t, pdCreate(t, cs, doc, "Cone", "Part::Cone", map[string]any{"Radius1": 9, "Radius2": 10, "Height": 1, "Placement": at}, nil))
	must(t, pdCreate(t, cs, doc, "Wedge", "Part::Cut", map[string]any{"Base": "Ring", "Tool": "Cone"}, nil))
	r := pdCreate(t, cs, doc, "Chamfered", "Part::Cut", map[string]any{"Base": "Knob", "Tool": "Wedge"}, nil)
	const removed = 30.3687 // pi * 10^2 * 1 - pi / 3 * (81 + 90 + 100)
	if v := replyVolume(t, r); v < 3141.6-removed-0.2 || v > 3141.6-removed+0.2 {
		t.Fatalf("chamfered knob = %g, want about %g", v, 3141.6-removed)
	}
	// A D-shaft hole: a cylinder intersected with a box, cut from the part.
	must(t, pdCreate(t, cs, doc, "Part", "Part::Box", map[string]any{"Length": 30, "Width": 30, "Height": 12}, nil))
	must(t, pdCreate(t, cs, doc, "Shaft", "Part::Cylinder", map[string]any{"Radius": 2.6, "Height": 20, "Placement": map[string]any{"Base": map[string]any{"x": 15, "y": 15, "z": -4}}}, nil))
	must(t, pdCreate(t, cs, doc, "Flat", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 20, "Placement": map[string]any{"Base": map[string]any{"x": 5, "y": 10, "z": -4}}}, nil))
	must(t, pdCreate(t, cs, doc, "DHole", "Part::Common", map[string]any{"Base": "Shaft", "Tool": "Flat"}, nil))
	r = pdCreate(t, cs, doc, "DPart", "Part::Cut", map[string]any{"Base": "Part", "Tool": "DHole"}, nil)
	if v := replyVolume(t, r); v < 10672.5 || v > 10672.7 {
		t.Fatalf("part with a D hole = %g, want 10672.6", v)
	}
	// A Body is laid out by its Placement: the features follow.
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "Plate", "PartDesign::AdditiveBox", map[string]any{"Length": 20, "Width": 10, "Height": 5}, nil))
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Body", "include_screenshot": false, "obj_properties": map[string]any{"Placement": map[string]any{"Base": map[string]any{"x": 0, "y": 0, "z": 10}}}}))
	v0416Code(t, cs, doc, "b = d.getObject('Body').Shape.BoundBox\nprint(b.ZMin, b.ZMax, d.getObject('Plate').Placement.Base.z)", "10.0 15.0 0.0")
}

// TestAlongFilterNeedsAStraightEdge: the cap arcs of a padded slot have end points that line up with y, but no edge of it runs along y.
func TestAlongFilterNeedsAStraightEdge(t *testing.T) {
	cs := session(t)
	const doc = "E2EAlongSlot"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "S", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane",
		"Geometry": []any{map[string]any{"slot": map[string]any{"center1": []any{10, 10}, "center2": []any{30, 10}, "width": 8}}}}, nil))
	must(t, pdCreate(t, cs, doc, "Pad", "PartDesign::Pad", map[string]any{"Profile": "S", "Length": 5}, nil))
	count := func(axis string) (int, reply) {
		r := call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": "Pad", "kind": "edges", "along": axis})
		return frontCount(t, r, "edges_matched"), r
	}
	if n, r := count("y"); n != 0 || strings.Contains(r.text, "| circle |") {
		t.Fatalf("along y matched %d edges:\n%s", n, r.text)
	}
	if n, r := count("x"); n != 4 || strings.Contains(r.text, "| circle |") {
		t.Fatalf("along x matched %d edges, want the 4 straight ones:\n%s", n, r.text)
	}
	if n, _ := count("z"); n != 4 {
		t.Fatalf("along z matched %d edges, want the 4 vertical ones", n)
	}
}
