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

func pdCreate(t *testing.T, cs *mcp.ClientSession, doc, name, typ string, props map[string]any, extra map[string]any) reply {
	t.Helper()
	args := map[string]any{"doc_name": doc, "obj_name": name, "obj_type": typ, "include_screenshot": false}
	if props != nil {
		args["obj_properties"] = props
	}
	for k, v := range extra {
		args[k] = v
	}
	return call(t, cs, "create_object", args)
}

// replyVolume reads "volume N mm^3" from a reply.
func replyVolume(t *testing.T, r reply) float64 {
	t.Helper()
	m := regexp.MustCompile(`volume ([0-9.]+) mm\^3`).FindStringSubmatch(r.text)
	if m == nil {
		t.Fatalf("reply has no volume:\n%s", r.text)
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

// TestPartDesignBodyThroughCreateObject: a Body, a sketch with a rectangle on the Body's XY plane, a Pad, a sketch on the
// Pad's top face (the ["Pad", "Face6"] form) with a circle, a Pocket through all and a PartDesign Fillet of the top edges,
// all through create_object, end in one valid solid inside the Body with the Fillet as Tip.
func TestPartDesignBodyThroughCreateObject(t *testing.T) {
	cs := session(t)
	const doc = "E2EPartDesign"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))

	// An empty sketch is not a failure; Support is the older name of AttachmentSupport.
	r := pdCreate(t, cs, doc, "Sketch", "Sketcher::SketchObject", map[string]any{"Support": "XY_Plane"}, nil)
	must(t, r, "In Body 'Body'.", "Sketch: no geometry yet.", "Note: Support is AttachmentSupport")
	if strings.Contains(r.text, "shape is null") {
		t.Fatalf("an empty sketch reads as a failure:\n%s", r.text)
	}
	r = call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Sketch", "include_screenshot": false,
		"obj_properties": map[string]any{"Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{0, 0}, "size": []any{40, 40}}}}}})
	must(t, r, "Sketch: 4 geometry elements, closed profile.")

	r = pdCreate(t, cs, doc, "Pad", "PartDesign::Pad", map[string]any{"Profile": "Sketch", "Length": 10}, nil)
	must(t, r, "In Body 'Body' (Tip: Pad).")
	if v := replyVolume(t, r); math.Abs(v-16000) > 1 {
		t.Fatalf("pad volume = %g, want 16000", v)
	}

	r = pdCreate(t, cs, doc, "HoleSketch", "Sketcher::SketchObject", map[string]any{
		"AttachmentSupport": []any{"Pad", "Face6"},
		"Geometry":          []any{map[string]any{"circle": map[string]any{"center": []any{20, 20}, "radius": 7.5}}}}, nil)
	must(t, r, "In Body 'Body' (Tip: Pad).", "Sketch: 1 geometry element, closed profile.")

	r = pdCreate(t, cs, doc, "Pocket", "PartDesign::Pocket", map[string]any{"Profile": "HoleSketch", "Type": "ThroughAll"}, nil)
	must(t, r, "In Body 'Body' (Tip: Pocket).")
	pocketVolume := 16000 - math.Pi*7.5*7.5*10
	if v := replyVolume(t, r); math.Abs(v-pocketVolume) > 1 {
		t.Fatalf("pocket volume = %g, want %g", v, pocketVolume)
	}

	// The four edges of the top face that are lines.
	edges := call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": "Pocket", "kind": "edges"})
	var top []any
	for _, line := range strings.Split(edges.text, "\n") {
		if strings.Contains(line, "| line |") && strings.Contains(line, ", 10) to (") && strings.Contains(line, ", 10), along") {
			top = append(top, strings.TrimSpace(strings.Split(line, "|")[1]))
		}
	}
	if len(top) != 4 {
		t.Fatalf("top edges = %v:\n%s", top, edges.text)
	}
	r = pdCreate(t, cs, doc, "Fillet", "PartDesign::Fillet", map[string]any{"Base": "Pocket", "Edges": top, "Radius": 1.5}, nil)
	must(t, r, "In Body 'Body' (Tip: Fillet).", "r1.5")
	loss := 4 * (1 - math.Pi/4) * 1.5 * 1.5 * 40
	if v := replyVolume(t, r); math.Abs(v-(pocketVolume-loss)) > 5 {
		t.Fatalf("fillet volume = %g, want about %g", v, pocketVolume-loss)
	}

	// Per-edge sizes are refused; Edges can be set again.
	fails(t, pdCreate(t, cs, doc, "Fillet2", "PartDesign::Fillet", map[string]any{"Base": "Pocket", "Edges": []any{map[string]any{"edge": "Edge4", "radius": 2}}, "Radius": 1}, nil),
		"takes one Radius for all its edges")
	r = call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Fillet", "include_screenshot": false, "obj_properties": map[string]any{"Edges": top[:2]}})
	must(t, r, "Edges now: ")

	// The Body holds all of it, the Tip is the Fillet, and nothing is invalid.
	v0416Code(t, cs, doc, "b = d.getObject('Body')\nprint([o.Name for o in b.Group], b.Tip.Name, d.getObject('Fillet').Shape.isValid(), len(d.getObject('Fillet').Shape.Solids))",
		"['Sketch', 'Pad', 'HoleSketch', 'Pocket', 'Fillet'] Fillet True 1")
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false}), "none invalid")
}

// TestPartDesignSeveralBodies: with several Bodies a feature or a bare origin plane needs body_name, which also picks the
// plane of that Body; a feature follows the Body its Profile is in.
func TestPartDesignSeveralBodies(t *testing.T) {
	cs := session(t)
	const doc = "E2EPartDesign2"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "Other", "PartDesign::Body", nil, nil))
	fails(t, pdCreate(t, cs, doc, "S1", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane"}, nil), "'XY_Plane' is ambiguous", "body_name")
	fails(t, pdCreate(t, cs, doc, "P1", "PartDesign::Pad", map[string]any{"Length": 3}, nil), "needs a Body", "Body, Other", "body_name")
	fails(t, pdCreate(t, cs, doc, "S1", "Sketcher::SketchObject", nil, map[string]any{"body_name": "Nope"}), "body_name 'Nope' is not a PartDesign::Body")
	fails(t, pdCreate(t, cs, doc, "B1", "Part::Box", nil, map[string]any{"body_name": "Body"}), "body_name is for PartDesign")

	must(t, pdCreate(t, cs, doc, "S2", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane",
		"Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{0, 0}, "size": []any{10, 10}}}}}, map[string]any{"body_name": "Other"}),
		"In Body 'Other'.")
	r := pdCreate(t, cs, doc, "P2", "PartDesign::Pad", map[string]any{"Profile": "S2", "Length": 5}, nil)
	must(t, r, "In Body 'Other' (Tip: P2).")
	v0416Code(t, cs, doc, "s = d.getObject('S2')\nprint(s.AttachmentSupport[0][0].Name, s.MapMode, d.getObject('Body').Group, round(d.getObject('P2').Shape.Volume, 3))",
		"XY_Plane001 FlatFace [] 500.0")
}

var peakText = regexp.MustCompile(`max von Mises = ([0-9.]+) MPa at \(([-0-9.]+), ([-0-9.]+), ([-0-9.]+)\), max displacement = ([0-9.]+) mm at \(([-0-9.]+), ([-0-9.]+), ([-0-9.]+)\)`)

// TestFemSaysWhereTheMaximaAre: on the reference cantilever the largest stress is at the fixed end (x = 0) on the top or
// bottom face and the largest displacement at the free end (x = 60).
func TestFemSaysWhereTheMaximaAre(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemPeaks"
	cantilever(t, cs, doc)
	r := runAnalysis(t, cs, doc)
	m := peakText.FindStringSubmatch(r.text)
	if m == nil {
		t.Fatalf("summary names no positions:\n%s", r.text)
	}
	num := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	if x, z := num(m[2]), num(m[4]); x > 3 || (z > 0.01 && z < 3.99) {
		t.Errorf("max von Mises at x = %g, z = %g, want the fixed end (x about 0) on the top or bottom face", x, z)
	}
	if x := num(m[6]); x < 59 {
		t.Errorf("max displacement at x = %g, want the free end (60)", x)
	}
	must(t, r, `"max_von_mises_at": [`, `"max_displacement_at": [`)
}

// TestFemRefusesConstraintsOnAnotherObjectThanTheMeshed: a Cut made after the constraints with the mesh pointed at it leaves
// the constraints on the old beam; the run says so, and passes once they name the Cut.
func TestFemRefusesConstraintsOnAnotherObjectThanTheMeshed(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemOffMesh"
	cantilever(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Hole", "Part::Cylinder", map[string]any{"Radius": 2, "Height": 4, "Placement": map[string]any{"Base": map[string]any{"x": 30, "y": 15, "z": 0}}}, nil))
	must(t, pdCreate(t, cs, doc, "HoleBeam", "Part::Cut", map[string]any{"Base": "Beam", "Tool": "Hole"}, nil))
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Mesh", "obj_properties": map[string]any{"Shape": "HoleBeam"}, "include_screenshot": false}))

	r := runAnalysis(t, cs, doc)
	fails(t, r, "references Beam, but Mesh meshes HoleBeam: point References at faces of HoleBeam (list_subelements)")

	// The Cut's faces are numbered by FreeCAD again: find the end face and the top face by their place.
	faces := call(t, cs, "list_subelements", map[string]any{"doc_name": doc, "obj_name": "HoleBeam", "kind": "faces"})
	for name, face := range map[string]string{"Fixed": faceAt(t, faces.text, 0, 0), "Load": faceAt(t, faces.text, 2, 4)} {
		must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": name, "include_screenshot": false,
			"obj_properties": map[string]any{"References": []any{map[string]any{"object_name": "HoleBeam", "faces": []any{face}}}}}))
	}
	r = runAnalysis(t, cs, doc)
	must(t, r, "solved")
	if v := frontFloat(t, r, "max_displacement_mm"); v < 0.1 {
		t.Fatalf("max displacement = %g, want a solved cantilever", v)
	}
}

// faceAt returns the name of the planar face of a list_subelements table whose centre has coordinate axis (0 x, 1 y,
// 2 z) equal to at.
func faceAt(t *testing.T, table string, axis int, at float64) string {
	t.Helper()
	centre := regexp.MustCompile(`center \(([-0-9.]+), ([-0-9.]+), ([-0-9.]+)\)`)
	for _, line := range strings.Split(table, "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 4 || !strings.HasPrefix(strings.TrimSpace(cells[1]), "Face") || !strings.Contains(line, "plane") {
			continue
		}
		if m := centre.FindStringSubmatch(line); m != nil {
			if v, _ := strconv.ParseFloat(m[axis+1], 64); math.Abs(v-at) < 1e-6 {
				return strings.TrimSpace(cells[1])
			}
		}
	}
	t.Fatalf("no plane face with coordinate %d = %g in:\n%s", axis, at, table)
	return ""
}

// TestAsyncStatusShowsThatACommitRan: commit(fn) stores into a module variable and get_async_status says it ran.
func TestAsyncStatusShowsThatACommitRan(t *testing.T) {
	cs := session(t)
	r := call(t, cs, "execute_code_async", map[string]any{
		"code": "def apply():\n    global fc19_result\n    fc19_result = 6 * 7\n    return fc19_result\ncommit(apply)"})
	must(t, r)
	job := frontValue(r.text, "job_id")
	if job == "" {
		t.Fatalf("no job id:\n%s", r.text)
	}
	var status reply
	for i := 0; i < 50; i++ {
		status = call(t, cs, "get_async_status", map[string]any{"job_id": job})
		if strings.Contains(status.text, ": done") {
			break
		}
	}
	must(t, status, "commit() ran 1 time(s); the last returned 42")
	must(t, call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "print(fc19_result)"}), "42")
}

// TestUpdatedGeometryReportsTheReplacedConstraints: update_object Geometry on a sketch that had constraints says how many went.
func TestUpdatedGeometryReportsTheReplacedConstraints(t *testing.T) {
	cs := session(t)
	const doc = "E2ESketchReplace"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	rect := func(w int) map[string]any {
		return map[string]any{"Geometry": []any{map[string]any{"rectangle": map[string]any{"corner": []any{0, 0}, "size": []any{w, w}}}}}
	}
	must(t, pdCreate(t, cs, doc, "Sketch", "Sketcher::SketchObject", rect(40), nil))
	r := call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Sketch", "obj_properties": rect(20), "include_screenshot": false})
	must(t, r, "Replaced the geometry and its 4 constraints.")
	must(t, pdCreate(t, cs, doc, "Plain", "Sketcher::SketchObject", nil, nil))
	r = call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Plain", "obj_properties": map[string]any{"Geometry": []any{map[string]any{"line": []any{[]any{0, 0}, []any{1, 0}}}}}, "include_screenshot": false})
	if strings.Contains(r.text, "Replaced") {
		t.Fatalf("a sketch with no constraints reports a replacement:\n%s", r.text)
	}
}

// TestBodyNameThatDiffersFromTheLinkedBodyIsRefused: a Pad whose Profile is in one Body is not put in another.
func TestBodyNameThatDiffersFromTheLinkedBodyIsRefused(t *testing.T) {
	cs := session(t)
	const doc = "E2EBodyMismatch"
	newDoc(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Body", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "Other", "PartDesign::Body", nil, nil))
	must(t, pdCreate(t, cs, doc, "S", "Sketcher::SketchObject", map[string]any{"AttachmentSupport": "XY_Plane"}, map[string]any{"body_name": "Body"}))
	fails(t, pdCreate(t, cs, doc, "Pad", "PartDesign::Pad", map[string]any{"Profile": "S", "Length": 5}, map[string]any{"body_name": "Other"}),
		"body_name 'Other' differs from the Body that Profile points into ('Body')", "Nothing was created")
}

// TestMeshOfACompoundTakesConstraintsOnItsChildren: constraints on the box inside a Part::Compound that the mesh meshes are
// the same solid; the run solves.
func TestMeshOfACompoundTakesConstraintsOnItsChildren(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemCompound"
	cantilever(t, cs, doc)
	must(t, pdCreate(t, cs, doc, "Comp", "Part::Compound", map[string]any{"Links": []any{"Beam"}}, nil))
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Mesh", "obj_properties": map[string]any{"Shape": "Comp"}, "include_screenshot": false}))
	r := runAnalysis(t, cs, doc)
	must(t, r, "solved")
	within(t, "max displacement", frontFloat(t, r, "max_displacement_mm"), 0.48, 0.10)
}

// TestAppLinkIsRefusedWhereFreeCADCrashesOnIt: References, Direction and a Gmsh mesh Shape given an App::Link are refused
// before FreeCAD sees them (it crashed with an access violation); nothing is created or changed and FreeCAD stays up.
func TestAppLinkIsRefusedWhereFreeCADCrashesOnIt(t *testing.T) {
	cs := session(t)
	const doc = "E2EFemLink"
	cantilever(t, cs, doc)
	v0416Code(t, cs, doc, "l = d.addObject('App::Link', 'Beam_link')\nl.LinkedObject = d.getObject('Beam')\ng = d.addObject('App::LinkGroup', 'Beam_group')\nd.recompute()\nprint(l.TypeId, g.TypeId)", "App::Link App::LinkGroup")
	fails(t, pdCreate(t, cs, doc, "Fixed3", "Fem::ConstraintFixed", map[string]any{"References": []any{map[string]any{"object_name": "Beam_group", "faces": []any{"Face1"}}}}, map[string]any{"analysis_name": "Analysis"}),
		"Beam_group is an App::LinkGroup", "Nothing was created")

	r := pdCreate(t, cs, doc, "Fixed2", "Fem::ConstraintFixed", map[string]any{"References": []any{map[string]any{"object_name": "Beam_link", "faces": []any{"Face1"}}}}, map[string]any{"analysis_name": "Analysis"})
	fails(t, r, "Beam_link is an App::Link: point References at Beam, the object it links", "Nothing was created")
	fails(t, pdCreate(t, cs, doc, "Mesh2", "Fem::FemMeshGmsh", map[string]any{"Shape": "Beam_link"}, map[string]any{"analysis_name": "Analysis"}),
		"Beam_link is an App::Link: point Shape at Beam", "Nothing was created")
	fails(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Fixed", "include_screenshot": false,
		"obj_properties": map[string]any{"References": []any{[]any{"Beam_link", "Face2"}}}}), "Beam_link is an App::Link")
	fails(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Mesh", "include_screenshot": false,
		"obj_properties": map[string]any{"Shape": "Beam_link"}}), "Beam_link is an App::Link")
	fails(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Load", "include_screenshot": false,
		"obj_properties": map[string]any{"Direction": []any{"Beam_link", "Edge5"}}}), "Beam_link is an App::Link")

	// FreeCAD is still up, nothing was made, and the constraint and mesh still name the beam.
	v0416Code(t, cs, doc, "print(d.getObject('Fixed2'), d.getObject('Mesh2'), d.getObject('Fixed').References[0][0].Name, d.getObject('Fixed').References[0][1], d.getObject('Mesh').Shape.Name)",
		"None None Beam ('Face1',) Beam")
}

// TestAFailedUpdateChangesNothing: a valid property set beside a bad one is not left applied (the transaction is aborted,
// not committed), the error says so, no undo step is left, and the document is up to date.
func TestAFailedUpdateChangesNothing(t *testing.T) {
	cs := session(t)
	const doc = "E2EAtomicUpdate"
	cantilever(t, cs, doc)
	state := "print(d.getObject('Beam').Length, d.getObject('Load').Force, d.getObject('Beam').State, d.UndoNames[0], len(d.UndoNames))"
	v0416Code(t, cs, doc, state, "60.0 mm", "['Up-to-date'] MCP: create_object")
	read := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "d = FreeCAD.getDocument('" + doc + "')\n" + state})
	before := read.text[strings.Index(read.text, "Output:"):]

	r := call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Beam", "include_screenshot": false,
		"obj_properties": map[string]any{"Length": 99, "Nonsense": 1}})
	fails(t, r, "Nonsense", "Nothing was changed.")
	r = call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Load", "include_screenshot": false,
		"obj_properties": map[string]any{"Force": "10 N", "References": []any{[]any{"Nobody", "Face1"}}}})
	fails(t, r, "Nobody", "Nothing was changed.")
	// A mesh update that fails does not mesh again.
	r = call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Mesh", "include_screenshot": false,
		"obj_properties": map[string]any{"CharacteristicLengthMax": 6, "Nonsense": 1}})
	fails(t, r, "Nothing was changed.")

	after := call(t, cs, "execute_code", map[string]any{"include_screenshot": false, "code": "d = FreeCAD.getDocument('" + doc + "')\n" + state +
		"\nprint(d.getObject('Mesh').CharacteristicLengthMax, d.getObject('Mesh').FemMesh.NodeCount)"})
	if got := after.text[strings.Index(after.text, "Output:"):]; !strings.HasPrefix(got, before[:len(before)-1]) || !strings.Contains(got, "3.0 mm 3746") {
		t.Fatalf("a failed update changed the document:\nbefore %s\nafter %s", before, got)
	}
	// A valid update still applies, as one undo step.
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Beam", "include_screenshot": false, "obj_properties": map[string]any{"Length": 70}}))
	v0416Code(t, cs, doc, state, "70.0 mm", "MCP: update_object")
}
