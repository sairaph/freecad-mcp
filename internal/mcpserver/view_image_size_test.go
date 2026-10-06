package mcpserver

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/sairaph/freecad-mcp/internal/xmlrpc/xmlrpctest"
)

func TestGetViewSaysTheImageSize(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 800, 281))); err != nil {
		t.Fatal(err)
	}
	fc := addon(t, map[string]xmlrpctest.Handler{
		"get_active_screenshot": func([]any) (any, error) {
			return map[string]any{"success": true, "document": "D", "image": base64.StdEncoding.EncodeToString(buf.Bytes())}, nil
		},
	})
	text := replyText(call(t, session(t, settingsFor(fc)), "get_view", map[string]any{"width": 800}))
	if !strings.Contains(text, "Image: 800 x 281 px.") {
		t.Errorf("reply = %s", text)
	}
}
