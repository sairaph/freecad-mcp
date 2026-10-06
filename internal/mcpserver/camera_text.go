package mcpserver

import (
	"fmt"
	"math"
	"strings"
)

// standardViews are FreeCAD's own views by the direction the camera looks
// along (measured on FreeCAD 1.1.3: set_view gives exactly these).
var standardViews = []struct {
	name string
	dir  [3]float64
}{
	{"Isometric", [3]float64{-0.5774, 0.5774, -0.5774}},
	{"Front", [3]float64{0, 1, 0}},
	{"Top", [3]float64{0, 0, -1}},
	{"Right", [3]float64{-1, 0, 0}},
	{"Back", [3]float64{0, -1, 0}},
	{"Left", [3]float64{1, 0, 0}},
	{"Bottom", [3]float64{0, 0, 1}},
	{"Dimetric", [3]float64{-0.3333, 0.8819, -0.3333}},
	{"Trimetric", [3]float64{-0.4096, 0.7094, -0.5736}},
}

// cameraText says where a camera looking along dir (three numbers) is: the
// direction to two decimals, the standard view when it is one, and the side
// the camera sits on, which is opposite to the direction it looks along.
func cameraText(v any) string {
	items, _ := v.([]any)
	var d [3]float64
	for i := range d {
		if i >= len(items) {
			return formatVector(v)
		}
		f, ok := number(items[i])
		if !ok {
			return formatVector(v)
		}
		d[i] = f
	}
	text := fmt.Sprintf("(%s, %s, %s)", rounded2(d[0]), rounded2(d[1]), rounded2(d[2]))
	norm := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
	if norm == 0 {
		return text
	}
	var notes []string
	for _, view := range standardViews {
		dot := (d[0]*view.dir[0] + d[1]*view.dir[1] + d[2]*view.dir[2]) /
			(norm * math.Sqrt(view.dir[0]*view.dir[0]+view.dir[1]*view.dir[1]+view.dir[2]*view.dir[2]))
		if dot > 0.999 {
			notes = append(notes, view.name)
			break
		}
	}
	// The camera sits at -d: a component under 0.2 is not worth naming.
	const side = 0.2
	switch {
	case d[2]/norm < -side:
		notes = append(notes, "from above")
	case d[2]/norm > side:
		notes = append(notes, "from below")
	}
	var sides []string
	for i, axis := range []string{"x", "y"} {
		switch {
		case d[i]/norm < -side:
			sides = append(sides, "+"+axis)
		case d[i]/norm > side:
			sides = append(sides, "-"+axis)
		}
	}
	if len(sides) > 0 {
		notes = append(notes, "from "+strings.Join(sides, " and "))
	}
	if len(notes) == 0 {
		return text
	}
	return text + ": " + strings.Join(notes, ", ")
}

func rounded2(f float64) string {
	return formatNumber(math.Round(f*100)/100 + 0)
}
