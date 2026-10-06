package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/xmlrpc/xmlrpctest"
)

func TestShapeLineGivesTheGlobalBoxAndShapesNowRowsDoNot(t *testing.T) {
	size := []any{7.0, 7.0, 5.0}
	span := []any{30.5, 37.5, -3.5, 3.5, 0.0, 5.0}
	shape := map[string]any{"solids": int64(1), "size": size, "volume": 189.8, "span": span}
	want := "1 solid, 7 x 7 x 5 mm at x 30.5 to 37.5, y -3.5 to 3.5, z 0 to 5, volume 189.8 mm^3"
	if got := shapeText(shape); got != want {
		t.Errorf("shapeText = %q, want %q", got, want)
	}
	row := map[string]any{"solids": int64(1), "size": size, "volume": 189.8}
	if got := shapeText(row); strings.Contains(got, " at ") {
		t.Errorf("a shape without a span says where it lies: %q", got)
	}
	fc := addon(t, map[string]xmlrpctest.Handler{"create_object": func([]any) (any, error) {
		return map[string]any{"success": true, "object_name": "Chamfer", "shape": shape,
			"adjusted": []any{map[string]any{"name": "Angle1", "given": int64(-90), "stored": 0.0}}}, nil
	}})
	text := replyText(call(t, session(t, settingsFor(fc)), "create_object",
		map[string]any{"doc_name": "D", "obj_name": "Chamfer", "obj_type": "Part::Chamfer", "include_screenshot": false}))
	for _, line := range []string{"Shape: " + want, "Angle1: given -90, FreeCAD stored 0 (outside the range it allows)."} {
		if !strings.Contains(text, line) {
			t.Errorf("reply lacks %q:\n%s", line, text)
		}
	}
}

func TestCreateObjectNamesTheSolidMakersAndStaysWithinItsLimit(t *testing.T) {
	text := toolTexts["create_object"].Params["obj_type"]
	for _, want := range []string{"Part::Loft (Sections, Solid true)", "Part::Sweep (Sections, Spine, Solid true)", "Part::Extrusion (Base, Dir, Solid true)"} {
		if !strings.Contains(text, want) {
			t.Errorf("obj_type lacks %q", want)
		}
	}
	if len(text) > maxParamDescriptionBytes {
		t.Errorf("obj_type is %d bytes, want at most %d", len(text), maxParamDescriptionBytes)
	}
	info := listAllTools(t)["create_object"]
	if info.schemaBytes > maxSchemaBytes {
		t.Errorf("create_object schema is %d bytes, want at most %d", info.schemaBytes, maxSchemaBytes)
	}
}

func TestStatusSaysWhetherSharingIsOn(t *testing.T) {
	_, off := sessionFront(context.Background(), map[string]any{"session": map[string]any{"enabled": false}})
	if off != "Sharing is off: agents on this computer share FreeCAD without a lock." {
		t.Errorf("off text = %q", off)
	}
	_, on := sessionFront(context.Background(), map[string]any{"session": map[string]any{"enabled": true, "held": false}})
	if !strings.Contains(on, "Sharing is on: one agent at a time holds FreeCAD.") || strings.Contains(on, "With remote access on") {
		t.Errorf("on text = %q", on)
	}
	text := statusText(t, func(int) (uint32, error) { return 1, nil })
	if !strings.Contains(text, "Sharing is off:") {
		t.Errorf("status reply lacks the sharing sentence:\n%s", text)
	}
}
