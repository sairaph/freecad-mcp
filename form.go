package main

// The form the app's Share and Connect pages are built on: toggles (○/●), text
// fields (bubbles/textinput: cursor, Left/Right/Home/End/Delete, masked
// password) and action rows (a bare ">"). Fields are always editable while
// focused, so there is no edit mode: tab and shift+tab move, space toggles a
// toggle, and the page decides what enter and esc do. A validation error shows
// under its field.

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type controlKind int

const (
	kindToggle controlKind = iota
	kindText
	kindAction
)

type control struct {
	id    string
	kind  controlKind
	label string
	// on is the state of a toggle.
	on bool
	// input is the field of a text control.
	input textinput.Model
	// err is the validation error shown under a text control.
	err string
	// hidden controls are skipped by the focus order and not drawn.
	hidden bool
	// digits makes a text control accept digits only.
	digits bool
	// indent nests a control under the row above it.
	indent bool
}

type form struct {
	p          palette
	controls   []*control
	focus      int
	labelWidth int
}

func newForm(p palette, controls ...*control) *form {
	f := &form{p: p, controls: controls, labelWidth: 18}
	for _, c := range controls {
		if n := utf8.RuneCountInString(c.label) + 2; c.kind != kindAction && n > f.labelWidth {
			f.labelWidth = n
		}
	}
	f.focusCurrent()
	return f
}

func (f *form) control(id string) *control {
	for _, c := range f.controls {
		if c.id == id {
			return c
		}
	}
	return nil
}

// current is the focused control.
func (f *form) current() *control { return f.controls[f.focus] }

func (f *form) focusCurrent() {
	for i, c := range f.controls {
		if c.kind != kindText {
			continue
		}
		if i == f.focus {
			c.input.Focus()
		} else {
			c.input.Blur()
		}
	}
}

// focusID moves the focus to the control with id, if it is shown.
func (f *form) focusID(id string) {
	for i, c := range f.controls {
		if c.id == id && !c.hidden {
			f.focus = i
			f.focusCurrent()
			return
		}
	}
}

// move moves the focus by delta (1 or -1) over the shown controls, wrapping.
func (f *form) move(delta int) {
	n := len(f.controls)
	for step := 0; step < n; step++ {
		f.focus = (f.focus + delta + n) % n
		if !f.controls[f.focus].hidden {
			break
		}
	}
	f.focusCurrent()
}

// setWidth sizes the text fields to what is left of the row.
func (f *form) setWidth(width int) {
	w := min(48, max(10, width-8-f.labelWidth))
	for _, c := range f.controls {
		if c.kind == kindText {
			c.input.Width = w
		}
	}
}

// handleKey handles tab, shift+tab, up, down, space on a toggle, and every
// other key as typing into the focused text field. It reports whether the key
// changed a toggle or a field (the page then clears that field's error).
func (f *form) handleKey(msg tea.KeyMsg) (changed bool, cmd tea.Cmd) {
	switch msg.String() {
	case "tab", "down":
		f.move(1)
		return false, nil
	case "shift+tab", "up":
		f.move(-1)
		return false, nil
	}
	c := f.current()
	switch c.kind {
	case kindToggle:
		if msg.Type == tea.KeySpace || msg.String() == " " {
			c.on = !c.on
			return true, nil
		}
	case kindText:
		if c.digits {
			var ok bool
			if msg, ok = digitsOnly(msg); !ok {
				return false, nil
			}
		}
		before := c.input.Value()
		c.input, cmd = c.input.Update(msg)
		if c.input.Value() != before {
			c.err = ""
			return true, cmd
		}
		return false, cmd
	}
	return false, nil
}

// view draws the controls: each row with the cursor on the focused one, and a
// text field's error in the error colour under it.
func (f *form) view(width int) []string {
	var lines []string
	for i, c := range f.controls {
		if c.hidden {
			continue
		}
		hl := i == f.focus
		label := c.label
		if c.indent {
			label = "  " + label
		}
		switch c.kind {
		case kindToggle:
			word := f.p.dim.Render("off")
			mark := "off"
			if c.on {
				word, mark = f.p.dim.Render("on"), "on"
			}
			lines = append(lines, markRow(f.p, hl, mark, label, f.labelWidth, word))
		case kindText:
			lines = append(lines, markRow(f.p, hl, "", label, f.labelWidth, c.input.View()))
			if c.err != "" {
				for _, l := range wrapText(c.err, max(10, width-8-f.labelWidth)) {
					lines = append(lines, strings.Repeat(" ", 6+f.labelWidth)+f.p.fail.Render(l))
				}
			}
		case kindAction:
			lines = append(lines, strings.TrimRight(markRow(f.p, hl, "", label, 0, ""), " "))
		}
	}
	return lines
}
