package mcpserver

import (
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/xmlrpc/xmlrpctest"
)

func TestCreateObjectNamesTheBodyTheTipAndWhatASketchHolds(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{
		"create_object": func(args []any) (any, error) {
			data, _ := args[1].(map[string]any)
			if data["Body"] != "Body2" {
				t.Errorf("body_name sent as %v", data["Body"])
			}
			return map[string]any{"success": true, "object_name": "Sketch",
				"body":   map[string]any{"name": "Body2", "tip": "Pad"},
				"sketch": map[string]any{"geometry_count": int64(4), "closed": true},
				"notes":  []any{"Support is AttachmentSupport in this FreeCAD; it was set as AttachmentSupport."}}, nil
		},
	})
	text := replyText(call(t, session(t, settingsFor(fc)), "create_object", map[string]any{"doc_name": "D", "obj_name": "Sketch",
		"obj_type": "Sketcher::SketchObject", "body_name": "Body2", "include_screenshot": false}))
	for _, want := range []string{"In Body 'Body2' (Tip: Pad).", "Sketch: 4 geometry elements, closed profile.",
		"Note: Support is AttachmentSupport in this FreeCAD"} {
		if !strings.Contains(text, want) {
			t.Errorf("reply lacks %q:\n%s", want, text)
		}
	}
}

func TestSketchTextSaysNoGeometryYetOrAnOpenProfile(t *testing.T) {
	for want, sketch := range map[string]map[string]any{
		"Sketch: no geometry yet.":                     {"geometry_count": int64(0), "closed": false},
		"Sketch: 1 geometry element, open profile.":    {"geometry_count": int64(1), "closed": false},
		"Sketch: 2 geometry elements, closed profile.": {"geometry_count": int64(2), "closed": true},
	} {
		if got := sketchText(sketch); got != want {
			t.Errorf("sketchText(%v) = %q, want %q", sketch, got, want)
		}
	}
}

func TestRunFEMAnalysisGivesWhereTheMaximaAre(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{"run_fem_analysis": func([]any) (any, error) {
		return map[string]any{"success": true, "result_object": "R", "node_count": int64(3746), "max_von_mises_MPa": 223.3, "max_displacement_mm": 0.45,
			"max_von_mises_at": []any{30.0, 7.5, 8.0}, "max_displacement_at": []any{60.0, 7.5, 3.99999}, "loads": []any{}, "working_dir": "C:/w"}, nil
	}})
	text := replyText(call(t, session(t, settingsFor(fc)), "run_fem_analysis", map[string]any{"doc_name": "D", "analysis_name": "A", "include_screenshot": false}))
	want := "max von Mises = 223.3 MPa at (30, 7.5, 8), max displacement = 0.45 mm at (60, 7.5, 4) (3746 nodes)."
	if !strings.Contains(text, want) {
		t.Errorf("reply lacks %q:\n%s", want, text)
	}
	if got := femAtText(nil); got != "" {
		t.Errorf("no position gave %q", got)
	}
}

func TestGetAsyncStatusSaysHowManyCommitsRan(t *testing.T) {
	fc := addon(t, map[string]xmlrpctest.Handler{"get_async_status": func([]any) (any, error) {
		return map[string]any{"success": true, "job": map[string]any{"id": "job-1", "state": "done", "commits": int64(2), "last_commit": "{'ok': True}"}}, nil
	}})
	text := replyText(call(t, session(t, settingsFor(fc)), "get_async_status", map[string]any{"job_id": "job-1"}))
	if !strings.Contains(text, "commit() ran 2 time(s); the last returned {'ok': True}") {
		t.Errorf("reply = %s", text)
	}
}
