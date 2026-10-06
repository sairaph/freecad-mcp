//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// buildChain makes Outer (Box) -> OuterFillet (4 vertical edges) -> Shell (Cut with Cavity) -> Body (Fuse with Boss).
func buildChain(t *testing.T, cs *mcp.ClientSession, doc string, outer map[string]any) {
	t.Helper()
	newDoc(t, cs, doc)
	create := func(name, typ string, props map[string]any) {
		must(t, call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": name, "obj_type": typ, "obj_properties": props, "include_screenshot": false}))
	}
	create("Outer", "Part::Box", outer)
	create("OuterFillet", "Part::Fillet", map[string]any{"Base": "Outer", "Edges": []any{"Edge1", "Edge3", "Edge6", "Edge12"}, "Radius": 1.5})
	create("Cavity", "Part::Box", map[string]any{"Length": 36, "Width": 26, "Height": 20, "Placement": map[string]any{"Base": map[string]any{"x": 2, "y": 2, "z": 2}}})
	create("Shell", "Part::Cut", map[string]any{"Base": "OuterFillet", "Tool": "Cavity"})
	create("Boss", "Part::Cylinder", map[string]any{"Radius": 3, "Height": 5, "Placement": map[string]any{"Base": map[string]any{"x": 10, "y": 10, "z": 2}}})
	create("Body", "Part::Fuse", map[string]any{"Base": "Shell", "Tool": "Boss"})
}

const notRebuilt = "Not rebuilt, still the shape from before: Shell, Body (they depend on OuterFillet, which failed)."

// TestObjectsBuiltOnAFailedOneAreNamedAsNotRebuilt: a fillet radius too large fails, FreeCAD leaves Shell and Body
// Up-to-date and Valid with their old shape, and every reply that lists the failure also names them.
func TestObjectsBuiltOnAFailedOneAreNamedAsNotRebuilt(t *testing.T) {
	cs := session(t)
	const doc = "E2EStale"
	buildChain(t, cs, doc, map[string]any{"Length": 40, "Width": 30, "Height": 20})

	r := call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "OuterFillet", "obj_properties": map[string]any{"Radius": 30}, "include_screenshot": false})
	fails(t, r, "OuterFillet", notRebuilt)

	// The states the design describes: the failed fillet alone is flagged, the objects above it look fine.
	v0416Code(t, cs, doc, "for n in ('OuterFillet', 'Shell', 'Body'):\n    o = d.getObject(n)\n    print(n, 'touched', 'Touched' in o.State, 'invalid', 'Invalid' in o.State, 'uptodate', 'Up-to-date' in o.State, 'valid', o.isValid())",
		"OuterFillet touched True invalid True uptodate False valid False", "Shell touched False invalid False uptodate True valid True", "Body touched False invalid False uptodate True valid True")

	rec := call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false})
	must(t, rec, "recomputed with 1 of 6 object(s) invalid", "OuterFillet (Part::Fillet)", notRebuilt, "stale_count: 2", "touched_count: 0")
	if strings.Contains(rec.text, "still touched") {
		t.Fatalf("the failed object was counted as touched:\n%s", rec.text)
	}

	// After the fix nothing is listed.
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "OuterFillet", "obj_properties": map[string]any{"Radius": 1.5}, "include_screenshot": false}))
	rec = call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false})
	must(t, rec, "stale_count: 0")
	if strings.Contains(rec.text, "Not rebuilt") || strings.Contains(rec.text, "invalid") && !strings.Contains(rec.text, "none invalid") {
		t.Fatalf("a fixed document still lists failures:\n%s", rec.text)
	}
}

// TestSheetDrivenFailureNamesWhatWasNotRebuilt: the box width comes from a sheet; a width the fillet radius does not fit
// fails the fillet, and update_spreadsheet_cells names Shell and Body.
func TestSheetDrivenFailureNamesWhatWasNotRebuilt(t *testing.T) {
	cs := session(t)
	const doc = "E2ESheetStale"
	buildChain(t, cs, doc, map[string]any{"Length": 40, "Width": 30, "Height": 20})
	must(t, call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": "Params", "obj_type": "Spreadsheet::Sheet", "include_screenshot": false}))
	set := func(content string) reply {
		return call(t, cs, "update_spreadsheet_cells", map[string]any{"doc_name": doc, "sheet_name": "Params", "cells": []any{map[string]any{"cell": "A1", "content": content, "alias": "w"}}})
	}
	must(t, set("30 mm"))
	must(t, call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Outer", "obj_properties": map[string]any{"Width": "=Params.w"}, "include_screenshot": false}))

	r := set("2 mm")
	must(t, r, "OuterFillet (Part::Fillet)", notRebuilt, "stale_count: 2")
	r = set("30 mm")
	must(t, r, "No objects became invalid")
	if strings.Contains(r.text, "Not rebuilt") {
		t.Fatalf("a repaired document lists stale objects:\n%s", r.text)
	}
}

// TestSeveralFailedObjectsGroupTheirDependents: two independent failures each get their own group.
func TestSeveralFailedObjectsGroupTheirDependents(t *testing.T) {
	cs := session(t)
	const doc = "E2ETwoStale"
	buildChain(t, cs, doc, map[string]any{"Length": 40, "Width": 30, "Height": 20})
	create := func(name, typ string, props map[string]any) {
		must(t, call(t, cs, "create_object", map[string]any{"doc_name": doc, "obj_name": name, "obj_type": typ, "obj_properties": props, "include_screenshot": false}))
	}
	create("Tall", "Part::Box", map[string]any{"Length": 10, "Width": 10, "Height": 10, "Placement": map[string]any{"Base": map[string]any{"x": 100, "y": 0, "z": 0}}})
	create("TallFillet", "Part::Fillet", map[string]any{"Base": "Tall", "Edges": []any{"Edge1", "Edge3"}, "Radius": 1})
	create("Cap", "Part::Cylinder", map[string]any{"Radius": 2, "Height": 2, "Placement": map[string]any{"Base": map[string]any{"x": 100, "y": 0, "z": 10}}})
	create("TallBody", "Part::Fuse", map[string]any{"Base": "TallFillet", "Tool": "Cap"})
	for _, name := range []string{"OuterFillet", "TallFillet"} {
		call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": name, "obj_properties": map[string]any{"Radius": 30}, "include_screenshot": false})
	}
	rec := call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false})
	must(t, rec, "stale_count: 3", "Shell, Body (they depend on OuterFillet, which failed); TallBody (it depends on TallFillet, which failed).")
}

// TestUpdateThatBreaksAnotherObjectReportsIt: a successful update_object of the box makes the fillet above it fail; the
// reply says so, names what was not rebuilt, and a later call does not repeat a failure that was there before.
func TestUpdateThatBreaksAnotherObjectReportsIt(t *testing.T) {
	cs := session(t)
	const doc = "E2ECollateral"
	buildChain(t, cs, doc, map[string]any{"Length": 40, "Width": 30, "Height": 20})
	update := func(props map[string]any) reply {
		return call(t, cs, "update_object", map[string]any{"doc_name": doc, "obj_name": "Outer", "obj_properties": props, "include_screenshot": false})
	}

	r := update(map[string]any{"Width": 2})
	must(t, r, "Object 'Outer' updated successfully.", "invalid_count: 1", "stale_count: 2", "1 other object(s) failed after this change:",
		"- OuterFillet (Part::Fillet): BRep_API: command not done. The radius is probably too large", notRebuilt)

	// OuterFillet already failed: a further change to the box is not blamed for it.
	r = update(map[string]any{"Length": 41})
	must(t, r, "Object 'Outer' updated successfully.")
	if strings.Contains(r.text, "failed after this change") || strings.Contains(r.text, "invalid_count") {
		t.Fatalf("an earlier failure was reported again:\n%s", r.text)
	}

	// The repair lists nothing, and a recompute still names the failure until it is fixed.
	r = update(map[string]any{"Width": 30})
	must(t, r, "Object 'Outer' updated successfully.")
	if strings.Contains(r.text, "failed after this change") || strings.Contains(r.text, "Not rebuilt") {
		t.Fatalf("a repaired document lists failures:\n%s", r.text)
	}
	must(t, call(t, cs, "recompute_document", map[string]any{"doc_name": doc, "include_screenshot": false}), "none invalid", "stale_count: 0")
}

// TestDeleteReportsWhatItBrokeAndUndoRedoReportOnce: deleting the fillet leaves the Cut above it failed and the Fuse
// above that holding its old shape; delete_object, redo list them once, undo lists nothing.
func TestDeleteReportsWhatItBrokeAndUndoRedoReportOnce(t *testing.T) {
	cs := session(t)
	const doc = "E2EDeleteStale"
	buildChain(t, cs, doc, map[string]any{"Length": 40, "Width": 30, "Height": 20})
	count := func(text string) int { return strings.Count(text, "Not rebuilt") }
	wantBroken := []string{"1 other object(s) failed after this change:", "- Shell (Part::Cut): Linked object is not a Part object.",
		"Not rebuilt, still the shape from before: Body (it depends on Shell, which failed).", "invalid_count: 1", "stale_count: 1"}

	r := call(t, cs, "delete_object", map[string]any{"doc_name": doc, "obj_name": "OuterFillet", "include_screenshot": false})
	must(t, r, append([]string{"Object 'OuterFillet' deleted successfully."}, wantBroken...)...)
	if count(r.text) != 1 {
		t.Fatalf("the delete reply repeats itself:\n%s", r.text)
	}

	r = call(t, cs, "undo", map[string]any{"doc_name": doc, "include_screenshot": false})
	must(t, r, "Undone 1 transaction(s)")
	if strings.Contains(r.text, "Not rebuilt") || strings.Contains(r.text, "invalid object") {
		t.Fatalf("undo lists failures after the repair:\n%s", r.text)
	}

	r = call(t, cs, "redo", map[string]any{"doc_name": doc, "include_screenshot": false})
	must(t, r, "Redone 1 transaction(s)", "1 invalid object(s):", "- Shell (Part::Cut): Linked object is not a Part object.", "Not rebuilt, still the shape from before: Body (it depends on Shell, which failed).")
	if count(r.text) != 1 || strings.Contains(r.text, "other object(s) failed") {
		t.Fatalf("redo reports twice or in the delete wording:\n%s", r.text)
	}

	// Deleting a box under the fillet fails the fillet; the Cut and Fuse above it are stale.
	buildChain(t, cs, "E2EDeleteBase", map[string]any{"Length": 40, "Width": 30, "Height": 20})
	must(t, call(t, cs, "delete_object", map[string]any{"doc_name": "E2EDeleteBase", "obj_name": "Outer", "include_screenshot": false}),
		"- OuterFillet (Part::Fillet): No object linked.", "Not rebuilt, still the shape from before: Shell, Body (they depend on OuterFillet, which failed).")
}
