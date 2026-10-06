package main

// The wizard's last screen, for a real run and a dry run alike. Once the AI
// clients are registered, this step applies the rest of what the wizard
// recorded (finishWizard) and shows one row per thing the user now has: AI
// clients, FreeCAD addon, Guide skill, Sharing, each with a status word and no
// absolute paths, then the next step. A dry run says first that no files were
// changed and shows the same rows as plans ("would install ...").

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sairaph/mcp-wizard/async"
	"github.com/sairaph/mcp-wizard/flow"
	"github.com/sairaph/mcp-wizard/harness"
	"github.com/sairaph/mcp-wizard/tui"

	"github.com/sairaph/freecad-mcp/internal/addoninstall"
	"github.com/sairaph/freecad-mcp/internal/domain"
)

// finishNextStep is what the user does after setup.
const finishNextStep = "Restart FreeCAD and your AI client, then ask it to build a part. Run `" + domain.BinaryName +
	"` to check the connection or change sharing later."

// finishSummary is what the finish screen shows.
type finishSummary struct {
	DryRun bool
	Rows   []statusRow
	// Code is the exit status of the steps behind the rows.
	Code int
}

// finishState is the finish step's state.
type finishState struct {
	Done    bool
	Summary finishSummary
}

// noteLines picks the lines of text a step wrote that tell the user
// something: failures and warnings (with their tag removed) and notes.
func noteLines(text string) []string {
	var notes []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, tag := range []string{"[fail]", "[warn]", "[note]"} {
			if rest, ok := strings.CutPrefix(line, tag); ok {
				notes = append(notes, strings.TrimSpace(rest))
			}
		}
	}
	return notes
}

// clientsRow summarises the AI client registration.
func clientsRow(state *AppState, dryRun bool) statusRow {
	row := statusRow{Label: "AI clients", Tone: toneOK}
	var done, already, skipped, failed int
	if dryRun {
		for _, c := range state.Results.Changes {
			switch c.State {
			case harness.ApplyNoop:
				already++
			case harness.ApplyFailed, harness.ApplyConflict:
				failed++
				row.Notes = append(row.Notes, strings.TrimSpace(c.Name+": "+c.Reason))
			case harness.ApplySkipped:
				skipped++
			default:
				done++
			}
		}
	} else {
		for _, r := range state.Results.Results {
			switch r.State {
			case harness.ApplyNoop:
				already++
			case harness.ApplyFailed, harness.ApplyConflict:
				failed++
				row.Notes = append(row.Notes, strings.TrimSpace(r.Name+": "+r.Reason))
			case harness.ApplySkipped:
				skipped++
			default:
				done++
			}
		}
	}
	var parts []string
	if done > 0 {
		verb := "registered"
		if dryRun {
			verb = "would register"
		}
		parts = append(parts, fmt.Sprintf("%s %d", verb, done))
	}
	if already > 0 {
		parts = append(parts, fmt.Sprintf("already registered %d", already))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d", skipped))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("failed %d", failed))
		row.Tone = toneFailed
	}
	if state.Failure != nil && failed == 0 {
		parts = append(parts, "failed")
		row.Tone = toneFailed
		row.Notes = append(row.Notes, state.Failure.Error())
	}
	if len(parts) == 0 {
		parts = []string{"none selected"}
	}
	row.Status = strings.Join(parts, ", ")
	return row
}

// addonRow installs the addon into every located folder (or plans it) and
// summarises the result. The exit code is 1 when an install failed.
func addonRow(a addonState, dryRun bool) (statusRow, int) {
	row := statusRow{Label: "FreeCAD addon", Tone: toneOK}
	if len(a.Targets) == 0 {
		row.Status, row.Tone = "FreeCAD not found", toneWarning
		row.Notes = []string{freecadNotFoundText}
		return row, 0
	}
	want, _, _ := addoninstall.EmbeddedVersion()
	var words []string
	code := 0
	if dryRun {
		for _, t := range a.Targets {
			if addonIsCurrent(t) {
				words = append(words, "already current "+want)
				continue
			}
			installed, _ := addoninstall.InstalledVersion(t)
			words = append(words, "would "+addonChangeWord(installed, want))
		}
	} else {
		for _, t := range a.Targets {
			r := installAddon(t, a.AutoStart)
			if r.Err != nil {
				words = append(words, "failed")
				row.Tone = toneFailed
				row.Notes = append(row.Notes, r.Err.Error())
				code = 1
				continue
			}
			words = append(words, addonVerb(r, want))
		}
	}
	row.Status = strings.Join(words, ", ")
	return row, code
}

// skillsRow installs the guide skill for ids' clients (or plans it). ok is
// false when no client reads a skill folder, so there is no row.
func skillsRow(scope harness.Scope, ids []harness.ID, dryRun bool) (row statusRow, ok bool, code int) {
	if len(ids) == 0 {
		return row, false, 0
	}
	row = statusRow{Label: "Guide skill", Tone: toneOK}
	home, err := os.UserHomeDir()
	if err != nil {
		row.Status, row.Tone = "failed", toneFailed
		row.Notes = []string{"cannot find the home directory: " + err.Error()}
		return row, true, 1
	}
	dirs, desktop := skillFolders(ids, scope, home)
	if len(dirs) == 0 && !desktop {
		return row, false, 0
	}
	var words []string
	seen := map[string]bool{}
	add := func(w string) {
		if !seen[w] {
			seen[w] = true
			words = append(words, w)
		}
	}
	for _, r := range syncSkillFolders(dirs, dryRun) {
		switch r.Kind {
		case skillLeft:
			add("left as it is")
			row.Notes = append(row.Notes, "A guide skill that freecad-mcp did not write is left as it is.")
		case skillCurrent:
			add("already current")
		case skillWould:
			add("would install")
		case skillWrote:
			add("installed")
		case skillFailed:
			add("failed")
			row.Tone = toneFailed
			row.Notes = append(row.Notes, r.Target+": "+r.Err.Error())
			code = 1
		}
	}
	if desktop {
		if len(words) == 0 {
			words = []string{"not installed for Claude Desktop"}
		}
		row.Notes = append(row.Notes, "Claude Desktop takes skills only as an upload (Settings > Capabilities > Skills), "+
			"so the guide is not installed there. The tools and the server instructions work without it.")
	}
	row.Status = strings.Join(words, ", ")
	return row, true, code
}

// sharingRow summarises the share choice. output is what the steps that
// applied it wrote: only its failures, warnings and notes are shown.
func sharingRow(sh shareState, dryRun, failed bool, output string, restarted bool) (statusRow, bool) {
	row := statusRow{Label: "Sharing", Tone: toneOK, Notes: noteLines(output)}
	if !sh.Asked {
		if !restarted {
			return row, false
		}
		row.Status = "listener restarted"
		return row, true
	}
	switch {
	case failed:
		row.Status, row.Tone = "failed", toneFailed
	case sh.On && dryRun:
		row.Status = "would turn on"
		row.Notes = append(row.Notes, fmt.Sprintf("Allowed devices %s, port %d.", sh.AllowedIPs, sh.Port))
	case sh.On:
		row.Status = "on"
		notes := shareOnNotes(sh.Port, sh.AllowedIPs)
		first := strings.TrimPrefix(notes[0], "Remote access on: ")
		notes[0] = strings.ToUpper(first[:1]) + first[1:] + "."
		row.Notes = append(row.Notes, notes...)
	case sh.Initial && dryRun:
		row.Status = "would turn off"
	default:
		row.Status = "off"
	}
	return row, true
}

// finishWizard applies what the wizard recorded once registration ran, in
// this order: the connection to FreeCAD on another computer or the share
// settings, the addon, then the listener. With nothing to change for the
// listener, a registered one is restarted so it runs this binary. The guide
// skill follows for the registered clients. It returns the screen's rows and
// the exit status.
func finishWizard(ctx context.Context, state *AppState, scope harness.Scope, dryRun bool, registrationCode int) finishSummary {
	sum := finishSummary{DryRun: dryRun}
	code := registrationCode
	clients := clientsRow(state, dryRun)

	var connectOut, shareOut bytes.Buffer
	connectCode := applyConnectChoice(ctx, &connectOut, state, dryRun)
	shareCode := applyShareChoice(ctx, &shareOut, state, dryRun)

	var addon statusRow
	var addonCode int
	if state.Connect.Chosen {
		hp := hostPort(state.Connect.Host, state.Connect.Port)
		addon = statusRow{Label: "Another computer", Tone: toneOK, Status: "saved " + hp}
		if dryRun {
			addon.Status = "would save " + hp
		}
		if connectCode != 0 {
			addon.Status, addon.Tone = "failed", toneFailed
			addon.Notes = noteLines(connectOut.String())
		}
	} else {
		addon, addonCode = addonRow(state.Addon, dryRun)
	}

	listenerCode, handled := applyShareListener(ctx, &shareOut, state, dryRun)
	shareCode = max(shareCode, listenerCode)
	restarted := false
	if !handled && !dryRun {
		var restartOut bytes.Buffer
		restartCode := restartListener(ctx, &restartOut)
		shareCode = max(shareCode, restartCode)
		shareOut.WriteString(restartOut.String())
		restarted = restartCode == 0 && strings.Contains(restartOut.String(), "restarted the listener")
	}

	skill, haveSkill, skillCode := skillsRow(scope, selectedIDs(state.Harness.Selected), dryRun)
	sharing, haveSharing := sharingRow(state.Share, dryRun, shareCode != 0, shareOut.String(), restarted)

	sum.Rows = append(sum.Rows, clients, addon)
	if haveSkill {
		sum.Rows = append(sum.Rows, skill)
	}
	if haveSharing {
		sum.Rows = append(sum.Rows, sharing)
	}
	sum.Code = max(code, connectCode, shareCode, addonCode, skillCode)
	return sum
}

// finishBody lays the finish screen out for width: the dry run notice, the
// rows, and the next step.
func finishBody(p palette, sum finishSummary, width int) []string {
	var body []string
	if sum.DryRun {
		body = append(body, "  "+p.warn.Render("Dry run: no files were changed."), "")
	}
	body = append(body, renderStatusRows(p, sum.Rows, width)...)
	if sum.Code != 0 {
		body = append(body, "")
		body = append(body, paragraph("Some steps did not finish; the rows above say which.", width, p.fail.Render)...)
	}
	body = append(body, "")
	body = append(body, paragraph(finishNextStep, width, nil)...)
	return body
}

// --- The step ---

type finishStep struct {
	ctx    context.Context
	header string
	scope  harness.Scope
	dryRun bool
	p      palette
}

func newFinishStep(ctx context.Context, header string, scope harness.Scope, dryRun bool) flow.Step[AppState] {
	return &finishStep{ctx: ctx, header: header, scope: scope, dryRun: dryRun, p: newPalette()}
}

func (s *finishStep) ID() string { return "finish" }

func (s *finishStep) Title(*AppState) string { return "" }

func (s *finishStep) Hints(state *AppState) []struct{ Key, Label string } {
	if !state.Finish.Done {
		return nil
	}
	return []struct{ Key, Label string }{{"enter", "to finish"}}
}

func (s *finishStep) Init(state *AppState) tea.Cmd {
	state.Finish = finishState{}
	code := 0
	if state.Failure != nil {
		code = 1
	}
	return tea.Batch(tui.Spinner(), async.Load(func() (finishSummary, error) {
		return finishWizard(s.ctx, state, s.scope, s.dryRun, code), nil
	}))
}

func (s *finishStep) Update(msg tea.Msg, state *AppState) (flow.Directive, tea.Cmd) {
	switch m := msg.(type) {
	case async.Result[finishSummary]:
		state.Finish = finishState{Done: true, Summary: m.Value}
		return flow.Continue, nil
	case tea.KeyMsg:
		switch m.String() {
		case "ctrl+c":
			// Quitting while the changes are being written would leave them
			// half done.
			if !state.Finish.Done && !s.dryRun {
				return flow.Continue, nil
			}
			return flow.Quit, nil
		case "enter":
			if state.Finish.Done {
				return flow.Next, nil
			}
		}
	}
	if tui.IsSpinMsg(msg) && !state.Finish.Done {
		state.Spinner.Frame++
		return flow.Continue, tui.Spinner()
	}
	return flow.Continue, nil
}

func (s *finishStep) View(state *AppState) string {
	size := screenSize{state.Width, state.Height}
	if !state.Finish.Done {
		verb := "Finishing setup..."
		if s.dryRun {
			verb = "Planning the rest of setup..."
		}
		return renderScreen(s.p, size, s.header, []string{"  " + tui.SpinFrame(state.Spinner.Frame) + " " + verb}, "")
	}
	return renderScreen(s.p, size, s.header, finishBody(s.p, state.Finish.Summary, size.width()),
		footerText(size.width(), hint{"enter", "to finish"}))
}
