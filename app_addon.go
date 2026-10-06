package main

// The app's "Install addon" page. It never changes anything on its first
// screen: it shows what an install would change (where, from which version to
// which, and the start-with-FreeCAD setting) with "Install" and "Cancel", and
// Cancel is highlighted. Enter on Install runs it and shows the summary
// report; esc or Cancel leaves with nothing changed.

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sairaph/mcp-wizard/async"
	"github.com/sairaph/mcp-wizard/tui"

	"github.com/sairaph/freecad-mcp/internal/addoninstall"
)

type addonPageStep int

const (
	addonPageLocating addonPageStep = iota
	addonPagePlan
	addonPageInstalling
	addonPageReport
)

// Plan actions, in screen order.
const (
	addonActionInstall = iota
	addonActionCancel
)

type addonPage struct {
	ctx  context.Context
	p    palette
	size screenSize

	phase        addonPageStep
	spinnerFrame int
	targets      []addoninstall.Target
	cursor       int
	report       *reportPage
}

func newAddonPage(ctx context.Context) appPage {
	return &addonPage{ctx: ctx, p: newPalette(), cursor: addonActionCancel}
}

// addonLocated carries the FreeCAD data folders the addon would go into.
type addonLocated struct{ targets []addoninstall.Target }

// addonInstalled carries the results of an install.
type addonInstalled struct{ results []addonResult }

func (a *addonPage) Init() tea.Cmd {
	ctx := a.ctx
	return tea.Batch(tui.Spinner(), async.Load(func() (addonLocated, error) {
		return addonLocated{targets: addoninstall.Locate(ctx, freecadCommand())}, nil
	}))
}

func (a *addonPage) Update(msg tea.Msg) (bool, tea.Cmd) {
	if a.report != nil {
		if wm, ok := msg.(tea.WindowSizeMsg); ok {
			a.size = screenSize{wm.Width, wm.Height}
		}
		return a.report.Update(msg)
	}
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.size = screenSize{m.Width, m.Height}
		return false, nil

	case async.Result[addonLocated]:
		a.targets = m.Value.targets
		if len(a.targets) == 0 {
			return false, a.showReport(reportLoad{rows: []reportRow{{
				Name: "FreeCAD addon", Outcome: toneFailed, Detail: freecadNotFoundText,
			}}})
		}
		a.phase = addonPagePlan
		return false, nil

	case async.Result[addonInstalled]:
		results := m.Value.results
		report := reportLoad{rows: addonResultRows(results)}
		if advice := addonRestartAdvice(results); advice != "" {
			report.notes = []string{advice}
		}
		return false, a.showReport(report)

	case tea.KeyMsg:
		switch a.phase {
		case addonPageLocating:
			return m.String() == "esc", nil
		case addonPagePlan:
			switch m.String() {
			case "esc":
				return true, nil
			case "up", "k":
				a.cursor = addonActionInstall
			case "down", "j":
				a.cursor = addonActionCancel
			case "enter":
				if a.cursor == addonActionCancel {
					return true, nil
				}
				return false, a.startInstall()
			}
		}
		return false, nil
	}

	if tui.IsSpinMsg(msg) && (a.phase == addonPageLocating || a.phase == addonPageInstalling) {
		a.spinnerFrame++
		return false, tui.Spinner()
	}
	return false, nil
}

// showReport replaces the plan with the summary report.
func (a *addonPage) showReport(report reportLoad) tea.Cmd {
	a.phase = addonPageReport
	a.report = finishedReportPage("Install addon", report)
	a.report.Update(tea.WindowSizeMsg{Width: a.size.w, Height: a.size.h})
	return nil
}

func (a *addonPage) startInstall() tea.Cmd {
	a.phase = addonPageInstalling
	targets := a.targets
	return tea.Batch(tui.Spinner(), async.Load(func() (addonInstalled, error) {
		var results []addonResult
		for _, t := range targets {
			results = append(results, installAddonWith(t, autoStartKeep))
		}
		return addonInstalled{results: results}, nil
	}))
}

func (a *addonPage) View() string {
	if a.report != nil {
		return a.report.View()
	}
	width := a.size.width()
	header := headerText("Install addon")
	switch a.phase {
	case addonPageLocating:
		body := []string{"  " + tui.SpinFrame(a.spinnerFrame) + " Asking FreeCAD where its addons live..."}
		return renderScreen(a.p, a.size, header, body, footerText(width, hint{"esc", "back"}))
	case addonPageInstalling:
		body := []string{"  " + tui.SpinFrame(a.spinnerFrame) + " Installing..."}
		return renderScreen(a.p, a.size, header, body, "")
	}

	body := paragraph("This installs the FreeCAD addon the MCP server talks to. Nothing changes until you choose Install.", width, nil)
	body = append(body, "")
	body = append(body, addonPlanLines(a.p, a.targets, width, true)...)
	body = append(body, "")
	for i, label := range []string{"Install", "Cancel"} {
		body = append(body, strings.TrimRight(markRow(a.p, a.cursor == i, "", label, 0, ""), " "))
	}
	return renderScreen(a.p, a.size, header, body,
		footerText(width, hint{"↑↓", "move"}, hint{"enter", "select"}, hint{"esc", "cancel"}))
}

// freecadNotFoundText is freecadNotFound as one paragraph.
var freecadNotFoundText = strings.Join(strings.Fields(freecadNotFound), " ")
