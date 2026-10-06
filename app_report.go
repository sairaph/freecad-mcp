package main

// The app's report screens (Doctor, Connection, the addon install summary): a
// summary line, then one row per check (see tuikit.go), in a viewport the size
// of the terminal less the header and the pinned footer. The report is built
// off the UI goroutine, so a slow check shows a spinner and never blocks the
// app.

import (
	"context"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sairaph/mcp-wizard/async"
	"github.com/sairaph/mcp-wizard/doctor"
	"github.com/sairaph/mcp-wizard/tui"
)

// reportLoad is a finished report: its rows and the notes under them.
type reportLoad struct {
	rows  []reportRow
	notes []string
}

// reportPage is an appPage showing a report built by load.
type reportPage struct {
	p     palette
	size  screenSize
	title string
	// load builds the report; it runs once, off the UI goroutine.
	load func() (reportLoad, error)

	loading      bool
	spinnerFrame int
	report       reportLoad
	err          error
	vp           viewport.Model
}

func newReportPage(title string, load func() (reportLoad, error)) *reportPage {
	return &reportPage{p: newPalette(), title: title, load: load, loading: true}
}

// finishedReportPage is a report already in hand.
func finishedReportPage(title string, report reportLoad) *reportPage {
	r := newReportPage(title, nil)
	r.loading = false
	r.report = report
	return r
}

func (r *reportPage) Init() tea.Cmd {
	if r.load == nil {
		return nil
	}
	load := r.load
	return tea.Batch(tui.Spinner(), async.Load(load))
}

// layout rebuilds the viewport for the current size and report.
func (r *reportPage) layout() {
	width := r.size.width()
	r.vp.Width = width
	r.vp.Height = max(1, r.size.height()-3)
	var lines []string
	switch {
	case r.err != nil:
		lines = paragraph("Failed: "+r.err.Error(), width, r.p.fail.Render)
	default:
		lines = renderReport(r.p, r.report.rows, width)
		for _, n := range r.report.notes {
			lines = append(lines, "")
			lines = append(lines, paragraph(n, width, nil)...)
		}
	}
	r.vp.SetContent(strings.Join(lines, "\n"))
}

func (r *reportPage) scrollable() bool { return r.vp.TotalLineCount() > r.vp.Height }

func (r *reportPage) Update(msg tea.Msg) (bool, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		r.size = screenSize{m.Width, m.Height}
		r.layout()
		return false, nil
	case async.Result[reportLoad]:
		r.loading = false
		r.report, r.err = m.Value, m.Err
		r.layout()
		r.vp.GotoTop()
		return false, nil
	case tea.KeyMsg:
		if m.String() == "esc" {
			return true, nil
		}
	}
	if tui.IsSpinMsg(msg) {
		if r.loading {
			r.spinnerFrame++
			return false, tui.Spinner()
		}
		return false, nil
	}
	if r.loading {
		return false, nil
	}
	var cmd tea.Cmd
	r.vp, cmd = r.vp.Update(msg)
	return false, cmd
}

func (r *reportPage) View() string {
	width := r.size.width()
	header := headerText(r.title)
	if r.loading {
		body := []string{"  " + tui.SpinFrame(r.spinnerFrame) + " Checking..."}
		return renderScreen(r.p, r.size, header, body, footerText(width, hint{"esc", "back"}))
	}
	r.layout()
	hints := []hint{}
	if r.scrollable() {
		hints = append(hints, hint{"↑↓", "scroll"}, hint{strconv.Itoa(int(r.vp.ScrollPercent()*100)) + "%", ""})
	}
	hints = append(hints, hint{"esc", "back"})
	return renderScreen(r.p, r.size, header, strings.Split(r.vp.View(), "\n"), footerText(width, hints...))
}

// --- The reports ---

// runChecks runs checks in order and returns their results, stopping with a
// failed "cancelled" row when ctx ends.
func runChecks(ctx context.Context, checks []doctor.Check) []doctor.Result {
	var results []doctor.Result
	for _, c := range checks {
		if c == nil {
			continue
		}
		if ctx.Err() != nil {
			results = append(results, doctor.Result{Name: "Checks", Status: doctor.Fail, Detail: "cancelled"})
			break
		}
		results = append(results, c.Run(ctx))
	}
	return results
}

func resultRows(results []doctor.Result) []reportRow {
	rows := make([]reportRow, 0, len(results))
	for _, r := range results {
		rows = append(rows, resultRow(r))
	}
	return rows
}

func doctorPage(ctx context.Context) *reportPage {
	return newReportPage("Doctor", func() (reportLoad, error) {
		return reportLoad{rows: resultRows(runChecks(ctx, doctorChecks()))}, nil
	})
}

func connectionPage(ctx context.Context) *reportPage {
	return newReportPage("Connection", func() (reportLoad, error) {
		results, _ := connectionResults(ctx)
		return reportLoad{rows: resultRows(results)}, nil
	})
}
