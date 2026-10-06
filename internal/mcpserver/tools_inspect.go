package mcpserver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sairaph/mcp-wizard/render"
)

type measureRef struct {
	Object string  `json:"object"`
	Sub    *string `json:"sub,omitempty"`
	Point  *string `json:"point,omitempty"`
}

type measureInput struct {
	DocName string       `json:"doc_name"`
	Kind    string       `json:"kind"`
	Refs    []measureRef `json:"refs"`
}

type getSelectionInput struct {
	DocName *string `json:"doc_name,omitempty"`
}

type measureFront struct {
	Document string   `yaml:"document"`
	Kind     string   `yaml:"kind"`
	Value    *float64 `yaml:"value,omitempty"`
	Unit     string   `yaml:"unit"`
}

type selectionFront struct {
	Count int `yaml:"count"`
}

type listSubelementsInput struct {
	DocName string `json:"doc_name"`
	ObjName string `json:"obj_name"`
	Kind    string `json:"kind,omitempty"`
	// The filters, applied in FreeCAD before the rows are built.
	Curve     *string  `json:"curve,omitempty"`
	Surface   *string  `json:"surface,omitempty"`
	Along     *string  `json:"along,omitempty"`
	OnBottom  *bool    `json:"on_bottom,omitempty"`
	Smooth    *bool    `json:"smooth,omitempty"`
	MinLength *float64 `json:"min_length,omitempty"`
}

// filters collects the filters that were given.
func (in listSubelementsInput) filters() map[string]any {
	out := map[string]any{}
	if in.Curve != nil {
		out["curve"] = *in.Curve
	}
	if in.Surface != nil {
		out["surface"] = *in.Surface
	}
	if in.Along != nil {
		out["along"] = *in.Along
	}
	if in.OnBottom != nil {
		out["on_bottom"] = *in.OnBottom
	}
	if in.Smooth != nil {
		out["smooth"] = *in.Smooth
	}
	if in.MinLength != nil {
		out["min_length"] = *in.MinLength
	}
	return out
}

type subelementsFront struct {
	Document string `yaml:"document"`
	Object   string `yaml:"object_name"`
	Kind     string `yaml:"kind"`
	Faces    *int   `yaml:"faces,omitempty"`
	Edges    *int   `yaml:"edges,omitempty"`
	// With filters, Faces and Edges stay the totals and these are the rows listed.
	FacesMatched *int `yaml:"faces_matched,omitempty"`
	EdgesMatched *int `yaml:"edges_matched,omitempty"`
}

func (s *Server) registerInspectTools() {
	schema := withMinItems(withEnum(inputSchema[measureInput](nil), "kind", "distance", "angle", "length", "radius", "area", "volume"), "refs", 1)
	addTool(s.mcpServer, "measure", schema, s.measure)
	addTool(s.mcpServer, "get_selection", inputSchema[getSelectionInput](nil), s.getSelection)
	addTool(s.mcpServer, "list_subelements",
		listSubelementsSchema(), s.listSubelements)
}

// refLabel names a ref the way a person would type it back: the object name,
// or "object.sub" when a sub-element was given.
func refLabel(r measureRef) string {
	if r.Sub != nil && *r.Sub != "" {
		return r.Object + "." + *r.Sub
	}
	return r.Object
}

// refsDescription describes what a measurement was taken of, for the two-ref
// kinds as "a and b", otherwise as a comma-separated list.
func refsDescription(refs []measureRef) string {
	labels := make([]string, len(refs))
	for i, r := range refs {
		labels[i] = refLabel(r)
	}
	if len(labels) == 2 {
		return labels[0] + " and " + labels[1]
	}
	return strings.Join(labels, ", ")
}

func (s *Server) measure(ctx context.Context, _ *mcp.CallToolRequest, in measureInput) (*mcp.CallToolResult, any, error) {
	conn, err := s.fc.get(ctx)
	if err != nil {
		return failure(ctx, "measure", err, ""), nil, nil
	}
	refs := make([]map[string]any, len(in.Refs))
	for i, r := range in.Refs {
		ref := map[string]any{"object": r.Object}
		if r.Sub != nil {
			ref["sub"] = *r.Sub
		}
		if r.Point != nil {
			ref["point"] = *r.Point
		}
		refs[i] = ref
	}
	res, err := conn.Measure(ctx, in.DocName, in.Kind, refs)
	if err != nil {
		return s.withNotice(failure(ctx, "measure", err, "")), nil, nil
	}
	if !succeeded(res) {
		return s.withNotice(reportedCode("measure", res,
			fmt.Sprintf("Call list_objects with {\"doc_name\": %q} to see the object and sub-element names.", in.DocName))), nil, nil
	}
	unit := str(res, "unit")
	front := measureFront{Document: in.DocName, Kind: in.Kind, Unit: unit}
	if v, ok := number(res["value"]); ok {
		front.Value = &v
	}

	var body strings.Builder
	fmt.Fprintf(&body, "%s of %s: %s.", in.Kind, refsDescription(in.Refs), formatMeasure(res["value"], unit))
	if used := stringItems(res["points_used"]); len(used) > 0 {
		fmt.Fprintf(&body, "\n\nMeasured between the %s.", strings.Join(used, " and the "))
	}
	if points, ok := res["points"].([]any); ok && len(points) == 2 {
		fmt.Fprintf(&body, "\n\nClosest points: %v to %v.", points[0], points[1])
	}
	body.WriteString("\n\n" + jsonBlock(res))

	out := render.SuccessResult(front, body.String())
	return s.withNotice(out), nil, nil
}

func (s *Server) listSubelements(ctx context.Context, _ *mcp.CallToolRequest, in listSubelementsInput) (*mcp.CallToolResult, any, error) {
	kind := in.Kind
	if kind == "" {
		kind = "faces"
	}
	conn, err := s.fc.get(ctx)
	if err != nil {
		return failure(ctx, "list subelements", err, ""), nil, nil
	}
	filters := in.filters()
	res, err := conn.ListSubelements(ctx, in.DocName, in.ObjName, kind, filters)
	if err != nil {
		return s.withNotice(failure(ctx, "list subelements", err, "")), nil, nil
	}
	if !succeeded(res) {
		return s.withNotice(reportedCode("list subelements", res,
			fmt.Sprintf("Call list_objects with {\"doc_name\": %q} to see the object names.", in.DocName))), nil, nil
	}
	front := subelementsFront{Document: in.DocName, Object: in.ObjName, Kind: kind}
	var body strings.Builder
	if faces, ok := res["faces"].([]any); ok {
		n := len(faces)
		front.Faces = &n
		if total, ok := res["face_count"].(int64); ok && len(filters) > 0 {
			front.Faces, front.FacesMatched = intPtr(int(total)), &n
		}
		body.WriteString(subelementTable("faces", "surface", "area mm^2", faces, faceDetails, noRowsText("faces", res["face_count"], filters)))
	}
	if edges, ok := res["edges"].([]any); ok {
		n := len(edges)
		front.Edges = &n
		if total, ok := res["edge_count"].(int64); ok && len(filters) > 0 {
			front.Edges, front.EdgesMatched = intPtr(int(total)), &n
		}
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		body.WriteString(subelementTable("edges", "curve", "length mm", edges, edgeDetails, noRowsText("edges", res["edge_count"], filters)))
	}
	if kind == "all" && facesUnfiltered(filters) {
		body.WriteString("\nFaces are not filtered: curve, along, smooth and min_length apply to edges (surface and on_bottom apply to faces).")
	}
	body.WriteString("\nPass a name as a measure ref's sub, or in a References entry such as {\"object_name\": " +
		fmt.Sprintf("%q", in.ObjName) + ", \"face\": \"Face1\"}.")
	return s.withNotice(render.SuccessResult(front, body.String())), nil, nil
}

func intPtr(n int) *int { return &n }

// facesUnfiltered reports whether the filters given are all edge filters, so a
// list of both kinds still holds every face.
func facesUnfiltered(filters map[string]any) bool {
	if len(filters) == 0 {
		return false
	}
	for name := range filters {
		if name == "surface" || name == "on_bottom" {
			return false
		}
	}
	return true
}

// listSubelementsSchema is the input schema of list_subelements with the enums of its filters.
func listSubelementsSchema() *jsonschema.Schema {
	s := inputSchema[listSubelementsInput](map[string]string{"kind": `"faces"`})
	s = withEnum(s, "kind", "faces", "edges", "all")
	s = withEnum(s, "curve", "line", "circle", "other")
	s = withEnum(s, "surface", "plane", "cylinder", "cone", "sphere", "torus", "other")
	s = withEnum(s, "along", "x", "y", "z")
	return s
}

// subelementTable renders rows (the addon's face or edge dicts) as a markdown
// table with name, the type column, the size column and a details column.
func subelementTable(what, typeHeader, sizeHeader string, rows []any, details func(map[string]any) string, none string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "| %s | %s | %s | details |\n|---|---|---|---|\n", strings.TrimSuffix(what, "s"), typeHeader, sizeHeader)
	for _, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		size := row["area"]
		if what == "edges" {
			size = row["length"]
		}
		typeKey := "surface"
		if what == "edges" {
			typeKey = "curve"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", str(row, "name"), str(row, typeKey), formatNumber(size), details(row))
	}
	if len(rows) == 0 {
		b.WriteString("\n" + none + "\n")
	}
	return b.String()
}

func faceDetails(row map[string]any) string {
	parts := []string{"center " + formatVector(row["center"])}
	if v, ok := row["normal"]; ok {
		parts = append(parts, "normal "+formatVector(v))
	}
	if v, ok := row["radius"]; ok {
		parts = append(parts, "radius "+formatNumber(v))
	}
	if v, ok := row["axis"]; ok {
		parts = append(parts, "axis "+formatVector(v))
	}
	if boolField(row, "on_bottom") {
		parts = append(parts, "on the bottom")
	}
	return strings.Join(parts, ", ")
}

func edgeDetails(row map[string]any) string {
	var parts []string
	if v, ok := row["start"]; ok {
		parts = append(parts, "from "+formatVector(v)+" to "+formatVector(row["end"]))
	}
	if v := str(row, "along"); v != "" {
		parts = append(parts, "along "+v)
	} else if v, ok := row["direction"]; ok {
		parts = append(parts, "direction "+formatVector(v))
	}
	if v, ok := row["radius"]; ok {
		parts = append(parts, "radius "+formatNumber(v))
	}
	if v, ok := row["center"]; ok {
		parts = append(parts, "center "+formatVector(v))
	}
	if boolField(row, "degenerate") {
		parts = append(parts, "degenerate (no length, a pole of a rounded corner): never pick it")
	}
	if boolField(row, "seam") {
		parts = append(parts, "smooth (a seam: the face meets itself): never fillet or chamfer it")
	} else if boolField(row, "smooth") {
		parts = append(parts, "smooth (its two faces meet without a corner): never fillet or chamfer it")
	}
	if boolField(row, "on_bottom") {
		parts = append(parts, "on the bottom")
	}
	return strings.Join(parts, ", ")
}

// formatNumber renders a numeric reply field, or "?" when it is missing (the
// addon sends null for a value that was not finite).
func formatNumber(v any) string {
	f, ok := number(v)
	if !ok {
		return "?"
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// formatVector renders a three-number reply field as "(x, y, z)".
func formatVector(v any) string {
	items, ok := v.([]any)
	if !ok {
		return "?"
	}
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = formatNumber(item)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func (s *Server) getSelection(ctx context.Context, _ *mcp.CallToolRequest, in getSelectionInput) (*mcp.CallToolResult, any, error) {
	conn, err := s.fc.get(ctx)
	if err != nil {
		return failure(ctx, "get selection", err, ""), nil, nil
	}
	doc := ""
	if in.DocName != nil {
		doc = *in.DocName
	}
	res, err := conn.GetSelection(ctx, doc)
	if err != nil {
		return s.withNotice(failure(ctx, "get selection", err, "")), nil, nil
	}
	if !succeeded(res) {
		return s.withNotice(reportedCode("get selection", res, "")), nil, nil
	}
	rows, _ := res["selection"].([]any)
	front := selectionFront{Count: len(rows)}

	var body strings.Builder
	if len(rows) == 0 {
		body.WriteString("Nothing is selected. Select an object or sub-element in FreeCAD, then call get_selection again.")
	} else {
		body.WriteString("| document | object | label | type | sub-elements |\n|---|---|---|---|---|\n")
		for _, item := range rows {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			subText := strings.Join(stringItems(row["sub_elements"]), ", ")
			if subText == "" {
				subText = "(whole object)"
			}
			fmt.Fprintf(&body, "| %s | %s | %s | %s | %s |\n", str(row, "document"), str(row, "object"), str(row, "label"), str(row, "type"), subText)
		}
		// The table leaves out the picked points (a click location per row);
		// they follow here in full.
		body.WriteString("\n" + jsonBlock(res))
	}

	out := render.SuccessResult(front, body.String())
	return s.withNotice(out), nil, nil
}

// noRowsText says why a table has no rows: the shape has none, or none match the filters.
func noRowsText(what string, total any, filters map[string]any) string {
	if n, ok := total.(int64); ok && len(filters) > 0 && n > 0 {
		return fmt.Sprintf("No %s match the filters (the shape has %d).", what, n)
	}
	return fmt.Sprintf("The shape has no %s.", what)
}
