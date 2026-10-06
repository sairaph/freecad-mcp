package mcpserver

import (
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/xmlrpc/xmlrpctest"
)

func TestImportNamesTheFileNotTheExtension(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{"import_file": func([]any) (any, error) {
		return map[string]any{"success": true, "document": "verify10", "format": "step", "importer": "ImportGui.insert", "object_count": int64(1), "created_document": true}, nil
	}})
	cs := session(t, settingsFor(fc))
	for _, path := range []string{"C:/Users/Wild/verify10.step", `C:\Users\Wild\verify10.step`, "verify10.step"} {
		text := replyText(call(t, cs, "import_file", map[string]any{"path": path, "include_screenshot": false}))
		if !strings.Contains(text, "Imported 'verify10.step' into new document 'verify10'") || strings.Contains(text, "'.step'") {
			t.Errorf("import reply for %q = %s", path, text)
		}
		if !strings.Contains(text, "file: ") {
			t.Errorf("front matter lacks the file for %q:\n%s", path, text)
		}
	}
}

func TestShapesNoteListsTheChangedShapesInTheShapeFormat(t *testing.T) {
	plate := map[string]any{"solids": int64(1), "size": []any{60.0, 56.0, 5.0}, "volume": 14137.7}
	one := map[string]any{"changed_shapes": []any{map[string]any{"name": "Corners", "shape": plate}}, "changed_shapes_count": int64(1)}
	if got, want := shapesNote(one), "\n\nShapes now: Corners: 1 solid, 60 x 56 x 5 mm, volume 14137.7 mm^3"; got != want {
		t.Errorf("one = %q, want %q", got, want)
	}
	two := map[string]any{"changed_shapes": []any{map[string]any{"name": "A", "shape": plate}, map[string]any{"name": "B", "shape": map[string]any{"null": true}}},
		"changed_shapes_count": int64(5), "changed_shapes_truncated": true}
	got := shapesNote(two)
	for _, want := range []string{"Shapes now:\n- A: 1 solid", "\n- B: none (the shape is null)", "\n- and 3 more"} {
		if !strings.Contains(got, want) {
			t.Errorf("several lacks %q:\n%s", want, got)
		}
	}
	if shapesNote(map[string]any{}) != "" || shapesNote(map[string]any{"changed_shapes": []any{}}) != "" {
		t.Error("a call that changed nothing must add nothing")
	}
}

func TestUndoRedoRecomputeSheetAndDeleteReportTheShapesTheyChanged(t *testing.T) {
	changed := map[string]any{"changed_shapes": []any{map[string]any{"name": "Body", "shape": map[string]any{"solids": int64(1), "size": []any{60.0, 56.0, 5.0}, "volume": 16800.0}}}, "changed_shapes_count": int64(1)}
	with := func(base map[string]any) func([]any) (any, error) {
		return func([]any) (any, error) {
			out := map[string]any{"success": true, "document": "D", "undo_names": []any{}, "redo_names": []any{}, "invalid_objects": []any{}, "invalid_count": int64(0),
				"recomputed": int64(2), "object_count": int64(3), "touched_objects": []any{}, "touched_count": int64(0), "empty_results": []any{}, "empty_count": int64(0),
				"stale_meshes": []any{}, "object_name": "Other", "sheet": "Params", "updated": []any{"A1"}, "cells": []any{}}
			for k, v := range base {
				out[k] = v
			}
			return out, nil
		}
	}
	fc := addon(t, map[string]xmlrpctest.Handler{
		"undo":                     with(map[string]any{"undone": []any{"MCP: update_spreadsheet_cells"}, "changed_shapes": changed["changed_shapes"]}),
		"redo":                     with(map[string]any{"redone": []any{"MCP: update_spreadsheet_cells"}, "changed_shapes": changed["changed_shapes"]}),
		"recompute_document":       with(map[string]any{"changed_shapes": changed["changed_shapes"]}),
		"update_spreadsheet_cells": with(map[string]any{"changed_shapes": changed["changed_shapes"]}),
		"delete_object":            with(map[string]any{"changed_shapes": changed["changed_shapes"]}),
	})
	cs := session(t, settingsFor(fc))
	calls := map[string]map[string]any{
		"undo": {"doc_name": "D"}, "redo": {"doc_name": "D"}, "recompute_document": {"doc_name": "D"},
		"update_spreadsheet_cells": {"doc_name": "D", "sheet_name": "Params", "cells": []any{map[string]any{"cell": "A1", "content": "56"}}},
		"delete_object":            {"doc_name": "D", "obj_name": "Other"},
	}
	for tool, args := range calls {
		if tool != "update_spreadsheet_cells" && tool != "recompute_document" {
			args["include_screenshot"] = false
		}
		text := replyText(call(t, cs, tool, args))
		if !strings.Contains(text, "Shapes now: Body: 1 solid, 60 x 56 x 5 mm, volume 16800.0 mm^3") {
			t.Errorf("%s reply lacks the shape:\n%s", tool, text)
		}
	}
}

func TestMeasureSendsPointAndNamesWhatItUsed(t *testing.T) {
	var sent []any
	fc := addon(t, map[string]xmlrpctest.Handler{"measure": func(args []any) (any, error) {
		sent = args
		return map[string]any{"success": true, "value": 31.0, "unit": "mm", "points_used": []any{"centre of Cut.Edge10 (circle of radius 1.6)", "centre of Cut.Edge11 (circle of radius 1.6)"},
			"points": []any{[]any{110.0, 70.0, 12.0}, []any{141.0, 70.0, 12.0}}}, nil
	}})
	text := replyText(call(t, session(t, settingsFor(fc)), "measure", map[string]any{"doc_name": "D", "kind": "distance", "refs": []any{
		map[string]any{"object": "Cut", "sub": "Edge10", "point": "center"}, map[string]any{"object": "Cut", "sub": "Edge11", "point": "center"}}}))
	if !strings.Contains(text, "Measured between the centre of Cut.Edge10 (circle of radius 1.6) and the centre of Cut.Edge11") {
		t.Errorf("reply = %s", text)
	}
	refs, _ := sent[2].([]any)
	if len(refs) != 2 {
		t.Fatalf("refs sent = %v", sent)
	}
	for _, r := range refs {
		if ref, _ := r.(map[string]any); ref["point"] != "center" {
			t.Errorf("ref sent without its point: %v", ref)
		}
	}
}
