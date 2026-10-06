package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sairaph/freecad-mcp/internal/freecad"
	"github.com/sairaph/mcp-wizard/render"
)

type recomputeDocumentInput struct {
	DocName string   `json:"doc_name"`
	Timeout *float64 `json:"timeout,omitempty"`
	screenshotOptions
}

type recomputeFront struct {
	Document     string `yaml:"document"`
	Recomputed   int    `yaml:"recomputed"`
	ObjectCount  int    `yaml:"object_count"`
	InvalidCount int    `yaml:"invalid_count"`
	TouchedCount int    `yaml:"touched_count"`
	EmptyCount   int    `yaml:"empty_count"`
	StaleMeshes  int    `yaml:"stale_meshes"`
	StaleCount   int    `yaml:"stale_count"`
}

func (s *Server) registerRecomputeTools() {
	addTool(s.mcpServer, "recompute_document",
		withPositiveMax(inputSchema[recomputeDocumentInput](mergeDefaults(screenshotDefaults, map[string]string{"timeout": "120"})),
			"timeout", freecad.DefaultMaxExecuteCodeTime), s.recomputeDocument)
}

func (s *Server) recomputeDocument(ctx context.Context, _ *mcp.CallToolRequest, in recomputeDocumentInput) (*mcp.CallToolResult, any, error) {
	if err := freecad.CheckTimeout(in.Timeout); err != nil {
		return failure(ctx, "recompute document", err, ""), nil, nil
	}
	conn, err := s.fc.get(ctx)
	if err != nil {
		return failure(ctx, "recompute document", err, ""), nil, nil
	}
	res, err := conn.RecomputeDocument(ctx, in.DocName, in.Timeout)
	if err != nil {
		return s.withNotice(timedFailure(ctx, "recompute document", err, largerTimeout("recompute_document"))), nil, nil
	}
	if !succeeded(res) {
		return s.withNotice(reportedCode("recompute document", res,
			"Call list_documents with {} to see the open documents.")), nil, nil
	}

	invalidObjs, _ := res["invalid_objects"].([]any)
	touchedObjs, _ := res["touched_objects"].([]any)
	objectCount := intField(res, "object_count")
	invalidCount := intField(res, "invalid_count")
	touchedCount := intField(res, "touched_count")
	emptyObjs, _ := res["empty_results"].([]any)
	emptyCount := intField(res, "empty_count")
	staleMeshes, _ := res["stale_meshes"].([]any)
	front := recomputeFront{
		Document:     in.DocName,
		Recomputed:   intField(res, "recomputed"),
		ObjectCount:  objectCount,
		InvalidCount: invalidCount,
		TouchedCount: touchedCount,
		EmptyCount:   emptyCount,
		StaleMeshes:  len(staleMeshes),
		StaleCount:   intField(res, "stale_count"),
	}

	invalidNames := make(map[string]bool, len(invalidObjs))
	var body strings.Builder
	if invalidCount == 0 {
		switch {
		case front.Recomputed == 0:
			fmt.Fprintf(&body, "Document '%s': no object needed a recompute; %d object(s), none invalid.", in.DocName, objectCount)
		case emptyCount == 0 && len(staleMeshes) == 0:
			fmt.Fprintf(&body, "Document '%s' recomputed cleanly: %d object(s), none invalid.", in.DocName, objectCount)
		default:
			fmt.Fprintf(&body, "Document '%s' recomputed: %d object(s), none invalid.", in.DocName, objectCount)
		}
	} else {
		fmt.Fprintf(&body, "Document '%s' recomputed with %d of %d object(s) invalid:\n", in.DocName, invalidCount, objectCount)
		for _, item := range invalidObjs {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			invalidNames[str(obj, "name")] = true
			body.WriteString("\n- " + invalidObjectRow(obj, in.DocName))
		}
		if boolField(res, "invalid_truncated") {
			fmt.Fprintf(&body, "\n\n(showing the first %d of %d; call recompute_document again after fixing some of these)", len(invalidObjs), invalidCount)
		}
	}
	for _, item := range staleMeshes {
		if m, ok := item.(map[string]any); ok {
			if str(m, "found_by") == "mesh" {
				fmt.Fprintf(&body, "\n\n%s: its mesh is not the one last made from %s (an undo restores the record, not the mesh); run_fem_analysis remeshes it.",
					str(m, "mesh"), str(m, "shape"))
				continue
			}
			fmt.Fprintf(&body, "\n\n%s: %s changed since it was meshed; run_fem_analysis remeshes it.", str(m, "mesh"), str(m, "shape"))
		}
	}
	if emptyCount > 0 {
		var rows []string
		for _, item := range emptyObjs {
			if obj, ok := item.(map[string]any); ok {
				rows = append(rows, fmt.Sprintf("%s (%s)", str(obj, "name"), str(obj, "reason")))
			}
		}
		fmt.Fprintf(&body, "\n\nEmpty or no-op results: %s", strings.Join(rows, ", "))
		if boolField(res, "empty_truncated") {
			fmt.Fprintf(&body, " (showing the first %d of %d)", len(emptyObjs), emptyCount)
		}
	}
	if note := staleNote(res); note != "" {
		body.WriteString("\n\n" + note)
	}
	body.WriteString(shapesNote(res))
	if touchedCount > 0 {
		// Only objects not listed as invalid are counted here: a failed object
		// is Touched too, and is already listed above. What is left was
		// skipped because an upstream dependency failed or otherwise blocked
		// it (Document::recompute skips the dependents of a failed object), or
		// was cut off by the cap on the invalid list.
		var names []string
		for _, item := range touchedObjs {
			if n, ok := item.(string); ok {
				names = append(names, n)
			}
		}
		fmt.Fprintf(&body, "\n\n%d object(s) are still touched after recompute: an upstream dependency failed "+
			"or otherwise blocked their recompute; fix or remove the blocking object. Not listed above as invalid: %s",
			touchedCount, strings.Join(names, ", "))
		if boolField(res, "touched_truncated") {
			fmt.Fprintf(&body, " (showing the first %d of %d touched)", len(touchedObjs), touchedCount)
		}
	}

	out := render.SuccessResult(front, body.String())
	return s.withNotice(s.screenshot(ctx, conn, out, in.IncludeScreenshot, viewString(in.ViewName), in.DocName)), nil, nil
}
