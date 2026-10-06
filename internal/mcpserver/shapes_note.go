package mcpserver

import (
	"fmt"
	"strings"
)

// shapesNote lists the top-level shapes a call changed (undo, redo,
// recompute_document, update_spreadsheet_cells, delete_object), one line each
// in the Shape format of create_object, or "" when it changed none. One object
// reads "Shapes now: Corners: 1 solid, ..."; several are listed under it.
func shapesNote(res map[string]any) string {
	rows, _ := res["changed_shapes"].([]any)
	var lines []string
	for _, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		shape, _ := row["shape"].(map[string]any)
		lines = append(lines, str(row, "name")+": "+shapeText(shape))
	}
	if len(lines) == 0 {
		return ""
	}
	more := ""
	if boolField(res, "changed_shapes_truncated") {
		more = fmt.Sprintf("\n- and %d more (get_object shows one).", intField(res, "changed_shapes_count")-len(lines))
	}
	if len(lines) == 1 && more == "" {
		return "\n\nShapes now: " + lines[0]
	}
	return "\n\nShapes now:\n- " + strings.Join(lines, "\n- ") + more
}
