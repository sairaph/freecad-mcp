package main

// Shared pieces of every screen of the app and the setup wizard: the palette
// (mcp-wizard's own colours), the full-terminal layout with the footer pinned
// to the last row, word wrapping at the terminal width, the report rows
// (name, coloured outcome word, detail) and the toggle and text field rows.
// Everything here is a pure function of its arguments so it can be tested
// without a terminal.

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sairaph/mcp-wizard/doctor"
	"github.com/sairaph/mcp-wizard/tui"
)

// Terminal size used until the first tea.WindowSizeMsg arrives.
const (
	defaultWidth  = 80
	defaultHeight = 24
)

// screenSize is the terminal size a screen keeps from the last
// tea.WindowSizeMsg.
type screenSize struct{ w, h int }

func (s screenSize) width() int {
	if s.w <= 0 {
		return defaultWidth
	}
	return s.w
}

func (s screenSize) height() int {
	if s.h <= 0 {
		return defaultHeight
	}
	return s.h
}

// palette is mcp-wizard's colour set as styles.
type palette struct {
	title, dim, cursor, ok, off, fail, warn lipgloss.Style
}

func newPalette() palette {
	c := tui.DefaultTheme.Colors
	return palette{
		title:  lipgloss.NewStyle().Bold(true).Foreground(c.Primary),
		dim:    lipgloss.NewStyle().Foreground(c.Secondary),
		cursor: lipgloss.NewStyle().Foreground(c.Primary),
		ok:     lipgloss.NewStyle().Foreground(c.Success),
		off:    lipgloss.NewStyle().Foreground(c.Secondary),
		fail:   lipgloss.NewStyle().Foreground(c.Error),
		warn:   lipgloss.NewStyle().Foreground(c.Warning),
	}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes colour sequences, for tests and width measuring.
func stripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// wrapText breaks text into lines of at most width runes at spaces; a word
// longer than the width is broken inside. Newlines in text start new lines.
func wrapText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, w := range words {
			for utf8.RuneCountInString(w) > width {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				r := []rune(w)
				out = append(out, string(r[:width]))
				w = string(r[width:])
			}
			switch {
			case line == "":
				line = w
			case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) <= width:
				line += " " + w
			default:
				out = append(out, line)
				line = w
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// hint is one footer entry.
type hint struct{ key, label string }

// footerText joins hints with a middle dot. When they do not fit width the
// first ones (movement) are dropped until they do, so the keys that leave the
// screen stay.
func footerText(width int, hints ...hint) string {
	for start := 0; start < len(hints); start++ {
		parts := make([]string, 0, len(hints)-start)
		for _, h := range hints[start:] {
			parts = append(parts, strings.TrimSpace(h.key+" "+h.label))
		}
		text := strings.Join(parts, " · ")
		if utf8.RuneCountInString(text) <= width-2 || start == len(hints)-1 {
			return text
		}
	}
	return ""
}

// headerText is the first row of an app screen: "freecad-mcp  <Screen>".
func headerText(screen string) string { return "freecad-mcp  " + screen }

// renderScreen lays out one full-terminal screen: the header on the first row,
// a blank row, the body clipped to what fits, blank padding, and the footer
// on the last row, so the footer never floats under the content. Rows are
// exactly the terminal height.
func renderScreen(p palette, size screenSize, header string, body []string, footer string) string {
	return renderScreenStyled(p, size, p.title.Render(header), body, footer)
}

// renderScreenStyled is renderScreen with a header that is already styled.
func renderScreenStyled(p palette, size screenSize, styledHeader string, body []string, footer string) string {
	height := size.height()
	rows := []string{tui.DefaultTheme.Indent + styledHeader, ""}
	capacity := max(0, height-3)
	for i, line := range body {
		if i >= capacity {
			break
		}
		rows = append(rows, line)
	}
	for len(rows) < height-1 {
		rows = append(rows, "")
	}
	rows = append(rows, p.dim.Render(tui.DefaultTheme.Indent+footer))
	if len(rows) > height {
		rows = rows[:height]
	}
	return strings.Join(rows, "\n")
}

// indentLines prefixes every line with the two-space gutter of the screens.
func indentLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if l == "" {
			continue
		}
		out[i] = "  " + l
	}
	return out
}

// paragraph wraps text to the screen width less the gutter and indents it.
func paragraph(text string, width int, style func(...string) string) []string {
	lines := wrapText(text, width-4)
	if style != nil {
		for i, l := range lines {
			lines[i] = style(l)
		}
	}
	return indentLines(lines)
}

// --- Reports ---

type tone string

const (
	toneOK      tone = "ok"
	toneFailed  tone = "failed"
	toneWarning tone = "warning"
)

// reportRow is one check of a report.
type reportRow struct {
	Name    string
	Outcome tone
	Detail  string
}

// resultRow converts a doctor result.
func resultRow(r doctor.Result) reportRow {
	row := reportRow{Name: r.Name, Detail: r.Detail, Outcome: toneOK}
	switch r.Status {
	case doctor.Fail:
		row.Outcome = toneFailed
	case doctor.Warn:
		row.Outcome = toneWarning
	}
	return row
}

// Report column widths: the name column, then the outcome word.
const (
	reportNameWidth    = 24
	reportOutcomeWidth = 7
	reportMinDetail    = 20
)

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// reportSummary is the one line on top of a report: "8 checks passed" or
// "1 of 8 checks failed", with the warnings counted after it.
func reportSummary(rows []reportRow) string {
	failed, warned := 0, 0
	for _, r := range rows {
		switch r.Outcome {
		case toneFailed:
			failed++
		case toneWarning:
			warned++
		}
	}
	var s string
	if failed > 0 {
		s = strconv.Itoa(failed) + " of " + count(len(rows), "check", "checks") + " failed"
	} else {
		s = count(len(rows), "check passed", "checks passed")
	}
	if warned > 0 {
		s += ", " + count(warned, "warning", "warnings")
	}
	return s
}

// failedFirst returns rows with the failures first, then the warnings, then
// the passes, each group in its original order.
func failedFirst(rows []reportRow) []reportRow {
	out := make([]reportRow, 0, len(rows))
	for _, o := range []tone{toneFailed, toneWarning, toneOK} {
		for _, r := range rows {
			if r.Outcome == o {
				out = append(out, r)
			}
		}
	}
	return out
}

func (p palette) outcomeStyle(o tone) lipgloss.Style {
	switch o {
	case toneFailed:
		return p.fail
	case toneWarning:
		return p.warn
	}
	return p.ok
}

// renderReport lays a report out for width: the summary line, a blank line,
// then one row per check: the name in a fixed-width column, the outcome word
// in its colour and the detail after it, wrapped under the detail column.
func renderReport(p palette, rows []reportRow, width int) []string {
	summaryStyle := p.ok
	for _, r := range rows {
		if r.Outcome == toneFailed {
			summaryStyle = p.fail
			break
		}
		if r.Outcome == toneWarning {
			summaryStyle = p.warn
		}
	}
	lines := []string{"  " + summaryStyle.Render(reportSummary(rows)), ""}

	nameWidth := reportNameWidth
	for _, r := range rows {
		nameWidth = max(nameWidth, utf8.RuneCountInString(r.Name))
	}
	// 2 gutter + name + space + outcome + space
	detailCol := 2 + nameWidth + 1 + reportOutcomeWidth + 1
	inline := width-detailCol >= reportMinDetail

	for _, r := range failedFirst(rows) {
		name := r.Name + strings.Repeat(" ", nameWidth-utf8.RuneCountInString(r.Name))
		word := string(r.Outcome) + strings.Repeat(" ", reportOutcomeWidth-utf8.RuneCountInString(string(r.Outcome)))
		head := "  " + name + " " + p.outcomeStyle(r.Outcome).Render(word)
		if r.Detail == "" {
			lines = append(lines, strings.TrimRight(head, " "))
			continue
		}
		if inline {
			detail := wrapText(r.Detail, width-detailCol)
			lines = append(lines, head+" "+detail[0])
			for _, l := range detail[1:] {
				lines = append(lines, strings.Repeat(" ", detailCol)+l)
			}
			continue
		}
		lines = append(lines, strings.TrimRight(head, " "))
		for _, l := range wrapText(r.Detail, width-6) {
			lines = append(lines, "      "+l)
		}
	}
	return lines
}

// statusRow is a label, a status word and an optional detail line: the rows
// of the addon install plan and the wizard's finish screen.
type statusRow struct {
	Label  string
	Status string
	Tone   tone
	// Notes are lines under the row (planned changes, failure details); they
	// never repeat the label.
	Notes []string
}

const statusLabelWidth = 22

// renderStatusRows lays rows out as "  Label   status", with the status word
// in its tone's colour and notes wrapped and indented under the label.
func renderStatusRows(p palette, rows []statusRow, width int) []string {
	var lines []string
	statusCol := 2 + statusLabelWidth + 1
	for _, r := range rows {
		label := r.Label + strings.Repeat(" ", max(0, statusLabelWidth-utf8.RuneCountInString(r.Label)))
		status := wrapText(r.Status, max(10, width-statusCol))
		if len(status) == 0 {
			status = []string{""}
		}
		lines = append(lines, "  "+label+" "+p.outcomeStyle(r.Tone).Render(status[0]))
		for _, l := range status[1:] {
			lines = append(lines, strings.Repeat(" ", statusCol)+p.outcomeStyle(r.Tone).Render(l))
		}
		for _, n := range r.Notes {
			for _, l := range wrapText(n, max(10, width-statusCol)) {
				lines = append(lines, strings.Repeat(" ", statusCol)+l)
			}
		}
	}
	return lines
}

// --- Rows with marks ---

// markRow renders one control row: the cursor ">" on the highlighted row, the
// state mark of a toggle (on ● or off ○, blank for other rows), the label and
// the value after it.
func markRow(p palette, highlighted bool, mark string, label string, labelWidth int, value string) string {
	cur := " "
	if highlighted {
		cur = p.cursor.Render(">")
	}
	m := " "
	switch mark {
	case "on":
		m = p.ok.Render("●")
	case "off":
		m = p.off.Render("○")
	}
	pad := ""
	if labelWidth > utf8.RuneCountInString(label) {
		pad = strings.Repeat(" ", labelWidth-utf8.RuneCountInString(label))
	}
	row := " " + cur + " " + m + " " + label + pad
	if value != "" {
		row += " " + value
	}
	return row
}

// newTextField builds a text field: the cursor always shown (it does not
// blink, so no timer runs), the placeholder in the faint colour and the
// password masked.
func newTextField(p palette, placeholder string, secret bool) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	// A width is needed: without one the placeholder shows its first letter only.
	ti.Width = 40
	ti.Placeholder = placeholder
	ti.PlaceholderStyle = p.dim
	ti.SetCursorMode(textinput.CursorStatic)
	if secret {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '*'
	}
	return ti
}

// digitsOnly strips every character but 0 to 9 from typed or pasted text, for
// the number fields (textinput's own Validate does not reject input). ok is
// false when nothing is left of a text key, which is then dropped.
func digitsOnly(msg tea.KeyMsg) (tea.KeyMsg, bool) {
	if msg.Type != tea.KeyRunes {
		return msg, true
	}
	var kept []rune
	for _, r := range msg.Runes {
		if r >= '0' && r <= '9' {
			kept = append(kept, r)
		}
	}
	msg.Runes = kept
	return msg, len(kept) > 0
}
