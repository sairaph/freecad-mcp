package freecad

import "context"

// Measure measures kind (distance, angle, length, radius, area, volume) over
// refs, each {"object", "sub"?}.
func (c *Connection) Measure(ctx context.Context, doc, kind string, refs []map[string]any) (map[string]any, error) {
	return c.callMap(ctx, c.timeout, "measure", doc, kind, refs)
}

// ListSubelements lists the faces, edges or both (kind "faces", "edges" or
// "all") of obj with their names and geometry, keeping only the rows that
// match filters (curve, surface, along, on_bottom, smooth, min_length).
func (c *Connection) ListSubelements(ctx context.Context, doc, obj, kind string, filters map[string]any) (map[string]any, error) {
	if len(filters) == 0 {
		// An addon from before the filters takes three arguments.
		return c.callMap(ctx, c.timeout, "list_subelements", doc, obj, kind)
	}
	return c.callMap(ctx, c.timeout, "list_subelements", doc, obj, kind, filters)
}

// GetSelection returns the selection of doc, or of every document when doc
// is "".
func (c *Connection) GetSelection(ctx context.Context, doc string) (map[string]any, error) {
	return c.callMap(ctx, c.timeout, "get_selection", optString(doc))
}
