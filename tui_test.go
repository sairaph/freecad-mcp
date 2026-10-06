package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sairaph/mcp-wizard/async"
	"github.com/sairaph/mcp-wizard/doctor"
	"github.com/sairaph/mcp-wizard/flow"
	"github.com/sairaph/mcp-wizard/harness"

	"github.com/sairaph/freecad-mcp/internal/addoninstall"
	"github.com/sairaph/freecad-mcp/internal/domain"
)

// plainLines renders view without colours, one string per row.
func plainLines(view string) []string {
	return strings.Split(stripANSI(view), "\n")
}

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestRenderScreenPutsHeaderFirstAndFooterLast(t *testing.T) {
	body := []string{"  one", "  two"}
	lines := plainLines(renderScreen(newPalette(), screenSize{60, 10}, headerText("Doctor"), body, "esc back"))
	if len(lines) != 10 {
		t.Fatalf("%d rows, want the terminal height 10", len(lines))
	}
	if lines[0] != "  freecad-mcp  Doctor" {
		t.Fatalf("first row = %q", lines[0])
	}
	if lines[1] != "" || lines[2] != "  one" || lines[3] != "  two" {
		t.Fatalf("body rows = %q", lines[1:4])
	}
	if !strings.Contains(lines[9], "esc back") {
		t.Fatalf("last row = %q, want the footer", lines[9])
	}
}

func TestRenderScreenClipsABodyThatDoesNotFit(t *testing.T) {
	var body []string
	for i := 0; i < 50; i++ {
		body = append(body, fmt.Sprintf("  row %d", i))
	}
	lines := plainLines(renderScreen(newPalette(), screenSize{60, 10}, headerText("Doctor"), body, "esc back"))
	if len(lines) != 10 || !strings.Contains(lines[9], "esc back") {
		t.Fatalf("rows %d, last %q", len(lines), lines[len(lines)-1])
	}
}

func TestRenderScreenUsesAFullTerminalBeforeTheSizeIsKnown(t *testing.T) {
	lines := plainLines(renderScreen(newPalette(), screenSize{}, headerText("Doctor"), nil, "esc back"))
	if len(lines) != defaultHeight {
		t.Fatalf("%d rows, want %d", len(lines), defaultHeight)
	}
}

func TestFooterTextDropsMovementFirst(t *testing.T) {
	hints := []hint{{"\u2191\u2193", "move"}, {"enter", "select"}, {"q", "quit"}}
	if got := footerText(80, hints...); got != "\u2191\u2193 move \u00b7 enter select \u00b7 q quit" {
		t.Fatalf("wide footer = %q", got)
	}
	if got := footerText(30, hints...); got != "enter select \u00b7 q quit" {
		t.Fatalf("narrow footer = %q", got)
	}
	if got := footerText(80, hint{"45%", ""}, hint{"esc", "back"}); got != "45% \u00b7 esc back" {
		t.Fatalf("footer with a bare key = %q", got)
	}
}

func TestWrapText(t *testing.T) {
	if got := wrapText("aaa bbb ccc", 7); len(got) != 2 || got[0] != "aaa bbb" || got[1] != "ccc" {
		t.Fatalf("wrapText = %q", got)
	}
	if got := wrapText("abcdefghij", 4); len(got) != 3 || got[0] != "abcd" || got[2] != "ij" {
		t.Fatalf("a long word = %q", got)
	}
}

func okRows(n int) []reportRow {
	var rows []reportRow
	for i := 0; i < n; i++ {
		rows = append(rows, reportRow{Name: fmt.Sprintf("Check %d", i), Outcome: toneOK, Detail: "fine"})
	}
	return rows
}

func TestReportSummary(t *testing.T) {
	if got := reportSummary(okRows(8)); got != "8 checks passed" {
		t.Fatalf("all passed = %q", got)
	}
	if got := reportSummary(okRows(1)); got != "1 check passed" {
		t.Fatalf("one check = %q", got)
	}
	rows := okRows(7)
	rows = append(rows, reportRow{Name: "Broken", Outcome: toneFailed})
	if got := reportSummary(rows); got != "1 of 8 checks failed" {
		t.Fatalf("one failed = %q", got)
	}
	rows[0].Outcome = toneWarning
	if got := reportSummary(rows); got != "1 of 8 checks failed, 1 warning" {
		t.Fatalf("a failure and a warning = %q", got)
	}
}

func TestResultRowMapsTheStatus(t *testing.T) {
	for status, want := range map[doctor.Status]tone{doctor.OK: toneOK, doctor.Warn: toneWarning, doctor.Fail: toneFailed} {
		if got := resultRow(doctor.Result{Name: "x", Status: status}).Outcome; got != want {
			t.Errorf("%s -> %s, want %s", status, got, want)
		}
	}
}

func TestReportListsFailuresFirstInAlignedColumns(t *testing.T) {
	rows := []reportRow{
		{Name: "Executable", Outcome: toneOK, Detail: "fine"},
		{Name: "FreeCAD addon", Outcome: toneFailed, Detail: strings.Repeat("it is not installed in the folder FreeCAD reads ", 4)},
		{Name: "Remote access (this PC)", Outcome: toneWarning, Detail: "not running"},
	}
	lines := plainLines(strings.Join(renderReport(newPalette(), rows, 80), "\n"))
	if strings.TrimSpace(lines[0]) != "1 of 3 checks failed, 1 warning" || lines[1] != "" {
		t.Fatalf("summary rows = %q", lines[:2])
	}
	if !strings.HasPrefix(lines[2], "  FreeCAD addon") {
		t.Fatalf("the failure is not first: %q", lines[2])
	}
	column := 2 + reportNameWidth + 1
	if strings.Index(lines[2], "failed") != column {
		t.Fatalf("outcome word at %d in %q, want %d", strings.Index(lines[2], "failed"), lines[2], column)
	}
	var sawWarning, sawOK bool
	for _, l := range lines {
		if utf8.RuneCountInString(l) > 80 {
			t.Errorf("row of %d runes: %q", utf8.RuneCountInString(l), l)
		}
		if strings.HasPrefix(l, "  Remote access (this PC)") {
			sawWarning = strings.Index(l, "warning") == column
		}
		if strings.HasPrefix(l, "  Executable") {
			sawOK = strings.Index(l, "ok") == column
		}
	}
	if !sawWarning || !sawOK {
		t.Fatalf("outcome words not in one column:\n%s", strings.Join(lines, "\n"))
	}
	// The wrapped detail sits under the detail column, not under the name.
	wrapped := 0
	for _, l := range lines[3:] {
		if strings.HasPrefix(l, strings.Repeat(" ", column+reportOutcomeWidth+1)) && strings.TrimSpace(l) != "" {
			wrapped++
		}
	}
	if wrapped == 0 {
		t.Fatalf("the long detail did not wrap under its column:\n%s", strings.Join(lines, "\n"))
	}
}

func TestReportOnANarrowTerminalPutsTheDetailBelow(t *testing.T) {
	rows := []reportRow{{Name: "FreeCAD addon", Outcome: toneFailed, Detail: "it is not installed in the folder FreeCAD reads"}}
	lines := plainLines(strings.Join(renderReport(newPalette(), rows, 40), "\n"))
	for _, l := range lines {
		if utf8.RuneCountInString(l) > 40 {
			t.Errorf("row of %d runes: %q", utf8.RuneCountInString(l), l)
		}
	}
	if !strings.HasPrefix(lines[2], "  FreeCAD addon") || !strings.HasPrefix(lines[3], "      ") {
		t.Fatalf("rows = %q", lines)
	}
}

func TestMenuHeaderCarriesTheState(t *testing.T) {
	name := domain.BinaryName + " 0.4.28"
	cases := []struct {
		info menuInfo
		want string
	}{
		{menuInfo{}, name},
		{menuInfo{freecad: triYes, sharing: triNo}, name + "  FreeCAD running \u00b7 sharing off"},
		{menuInfo{freecad: triNo, sharing: triYes}, name + "  FreeCAD not running \u00b7 sharing on"},
		{menuInfo{freecad: triYes, sharing: triNo, host: "studio"}, name + "  FreeCAD running \u00b7 sharing off \u00b7 using studio"},
	}
	for _, c := range cases {
		if got := menuHeaderText("0.4.28", c.info); got != c.want {
			t.Errorf("header = %q, want %q", got, c.want)
		}
	}
}

func TestMenuListsTheShortEntriesAndPinsItsFooter(t *testing.T) {
	m := &menuScreen{p: newPalette(), size: screenSize{100, 20}}
	lines := plainLines(m.view())
	if len(lines) != 20 {
		t.Fatalf("%d rows, want 20", len(lines))
	}
	if !strings.HasPrefix(lines[0], "  "+domain.BinaryName+" "+version) {
		t.Fatalf("first row = %q", lines[0])
	}
	want := []string{"Doctor", "Install addon", "Connection", "Share this PC", "Use another computer", "Quit"}
	for i, label := range want {
		if row := lines[2+i]; !strings.HasSuffix(row, label) {
			t.Errorf("menu row %d = %q, want %q", i, row, label)
		}
	}
	if !strings.Contains(lines[2], ">") || strings.Contains(lines[3], ">") {
		t.Fatalf("the cursor is not on the first entry only: %q %q", lines[2], lines[3])
	}
	if !strings.Contains(lines[19], "q quit") {
		t.Fatalf("last row = %q", lines[19])
	}
}

func locatedAddonPage(t *testing.T) *addonPage {
	t.Helper()
	page := newAddonPage(context.Background()).(*addonPage)
	page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page.Update(async.Result[addonLocated]{Value: addonLocated{targets: []addoninstall.Target{{UserDataDir: t.TempDir()}}}})
	return page
}

func TestAddonInstallShowsAPlanWithCancelHighlighted(t *testing.T) {
	page := locatedAddonPage(t)
	if page.phase != addonPagePlan || page.cursor != addonActionCancel {
		t.Fatalf("phase %v, cursor %d: want the plan with Cancel highlighted", page.phase, page.cursor)
	}
	view := stripANSI(page.View())
	want, _, _ := addoninstall.EmbeddedVersion()
	for _, text := range []string{"Install into", "install " + want, "Start with FreeCAD", "Nothing changes until you choose Install"} {
		if !strings.Contains(view, text) {
			t.Errorf("plan lacks %q:\n%s", text, view)
		}
	}
	var install, cancel string
	for _, l := range strings.Split(view, "\n") {
		switch strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), ">")) {
		case "Install":
			install = l
		case "Cancel":
			cancel = l
		}
	}
	if !strings.Contains(cancel, ">") || strings.Contains(install, ">") || install == "" {
		t.Fatalf("cursor rows: install %q, cancel %q", install, cancel)
	}
}

func TestAddonPlanCancelAndEscChangeNothing(t *testing.T) {
	page := locatedAddonPage(t)
	if done, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter}); !done || cmd != nil {
		t.Fatalf("enter on Cancel: done %v, cmd %v", done, cmd != nil)
	}
	page = locatedAddonPage(t)
	page.Update(tea.KeyMsg{Type: tea.KeyUp})
	if page.cursor != addonActionInstall {
		t.Fatalf("up moved to %d", page.cursor)
	}
	page.Update(tea.KeyMsg{Type: tea.KeyDown})
	if done, _ := page.Update(tea.KeyMsg{Type: tea.KeyEsc}); !done {
		t.Fatal("esc did not leave the plan")
	}
}

func TestAddonInstallRunsOnlyFromTheInstallRow(t *testing.T) {
	page := locatedAddonPage(t)
	page.Update(tea.KeyMsg{Type: tea.KeyUp})
	done, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if done || cmd == nil || page.phase != addonPageInstalling {
		t.Fatalf("enter on Install: done %v, cmd %v, phase %v", done, cmd != nil, page.phase)
	}
}

func TestAddonResultsBecomeReportRows(t *testing.T) {
	target := addoninstall.Target{UserDataDir: t.TempDir()}
	results := []addonResult{installAddon(target, true), {Target: target, Err: errors.New("disk full")}}
	rows := addonResultRows(results)
	if rows[0].Outcome != toneOK || !strings.HasPrefix(rows[0].Detail, "installed ") {
		t.Fatalf("installed row = %+v", rows[0])
	}
	if rows[1].Outcome != toneFailed || !strings.Contains(rows[1].Detail, "disk full") {
		t.Fatalf("failed row = %+v", rows[1])
	}
}

func TestAddonChangeWord(t *testing.T) {
	for _, c := range []struct{ installed, want, word string }{
		{"", "0.4.28", "install 0.4.28"},
		{"0.4.28", "0.4.28", "reinstall 0.4.28"},
		{"0.4.26", "0.4.28", "update 0.4.26 to 0.4.28"},
	} {
		if got := addonChangeWord(c.installed, c.want); got != c.word {
			t.Errorf("addonChangeWord(%q, %q) = %q, want %q", c.installed, c.want, got, c.word)
		}
	}
}

func sizedReport(rows []reportRow, w, h int) *reportPage {
	page := finishedReportPage("Doctor", reportLoad{rows: rows})
	page.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return page
}

func TestReportPageShowsThePercentOnlyWhenItScrolls(t *testing.T) {
	short := plainLines(sizedReport(okRows(3), 100, 30).View())
	if len(short) != 30 || strings.Contains(short[29], "%") || strings.Contains(short[29], "scroll") {
		t.Fatalf("a report that fits shows scrolling: %q", short[29])
	}
	if strings.TrimSpace(short[0]) != "freecad-mcp  Doctor" || !strings.Contains(short[29], "esc back") {
		t.Fatalf("header %q, footer %q", short[0], short[29])
	}
	long := plainLines(sizedReport(okRows(40), 100, 12).View())
	if len(long) != 12 || !strings.Contains(long[11], "%") || !strings.Contains(long[11], "scroll") {
		t.Fatalf("a report that scrolls: %q", long[11])
	}
}

func TestFailedReportShowsTheError(t *testing.T) {
	page := newReportPage("Doctor", nil)
	page.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	page.Update(async.Result[reportLoad]{Err: errors.New("boom")})
	if view := stripANSI(page.View()); !strings.Contains(view, "Failed: boom") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestFinishScreenOfADryRunHasNoPaths(t *testing.T) {
	sum := finishSummary{DryRun: true, Rows: []statusRow{
		{Label: "AI clients", Status: "would register 2", Tone: toneOK},
		{Label: "FreeCAD addon", Status: "would install 0.4.28", Tone: toneOK},
		{Label: "Guide skill", Status: "would install", Tone: toneOK},
		{Label: "Sharing", Status: "would turn off", Tone: toneOK},
	}}
	lines := plainLines(strings.Join(finishBody(newPalette(), sum, 80), "\n"))
	if strings.TrimSpace(lines[0]) != "Dry run: no files were changed." {
		t.Fatalf("first row = %q", lines[0])
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{"AI clients", "FreeCAD addon", "Guide skill", "Sharing", "Restart FreeCAD and your AI client, then ask it to build a part."} {
		if !strings.Contains(text, want) {
			t.Errorf("finish screen lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `:\`) || strings.Contains(text, "/home") || strings.Contains(text, "~") {
		t.Fatalf("the finish screen shows a path:\n%s", text)
	}
	for _, l := range lines {
		if utf8.RuneCountInString(l) > 80 {
			t.Errorf("row of %d runes: %q", utf8.RuneCountInString(l), l)
		}
	}
	sum.DryRun = false
	if first := plainLines(strings.Join(finishBody(newPalette(), sum, 80), "\n"))[0]; strings.Contains(first, "Dry run") {
		t.Fatalf("a real run says it is a dry run: %q", first)
	}
}

func TestClientsRowCountsTheRegistration(t *testing.T) {
	state := &AppState{}
	state.Results.Results = []harness.Result{{State: harness.Applied}, {State: harness.Applied}, {State: harness.ApplyNoop}}
	if row := clientsRow(state, false); row.Status != "registered 2, already registered 1" || row.Tone != toneOK {
		t.Fatalf("real run row = %+v", row)
	}
	state = &AppState{}
	state.Results.Changes = []harness.Change{{State: harness.Applied, Action: "register"}}
	if row := clientsRow(state, true); row.Status != "would register 1" {
		t.Fatalf("dry run row = %+v", row)
	}
	state = &AppState{}
	state.Results.Results = []harness.Result{{Name: "Cursor", State: harness.ApplyFailed, Reason: "locked"}}
	if row := clientsRow(state, false); row.Tone != toneFailed || len(row.Notes) != 1 || row.Notes[0] != "Cursor: locked" {
		t.Fatalf("failed row = %+v", row)
	}
	if row := clientsRow(&AppState{}, false); row.Status != "none selected" {
		t.Fatalf("empty row = %+v", row)
	}
}

func TestSharingRowKeepsPlannedChangesUnderTheLabel(t *testing.T) {
	row, ok := sharingRow(shareState{Asked: true, Initial: true}, true, false, "", false)
	if !ok || row.Label != "Sharing" || row.Status != "would turn off" {
		t.Fatalf("row = %+v", row)
	}
	row, _ = sharingRow(shareState{Asked: true, On: true, Port: 9876, AllowedIPs: "127.0.0.1"}, true, false, "", false)
	if row.Status != "would turn on" || len(row.Notes) == 0 {
		t.Fatalf("dry run on = %+v", row)
	}
	row, _ = sharingRow(shareState{Asked: true}, false, true, "  [fail] stop the listener: boom\n  [ok] fine\n", false)
	if row.Tone != toneFailed || len(row.Notes) != 1 || row.Notes[0] != "stop the listener: boom" {
		t.Fatalf("failed row = %+v", row)
	}
	if _, ok := sharingRow(shareState{}, false, false, "", false); ok {
		t.Fatal("a row for a question that was not asked")
	}
}

func TestShareQuestionIsAToggleRowWithTheAddonFooter(t *testing.T) {
	step := &shareChoiceStep{}
	state := &AppState{}
	view := stripANSI(step.View(state))
	if !strings.Contains(view, "\u25cb Share this PC with other devices") {
		t.Fatalf("off state:\n%s", view)
	}
	step.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, state)
	view = stripANSI(step.View(state))
	if !state.Share.On || !strings.Contains(view, "\u25cf Share this PC with other devices") {
		t.Fatalf("on state:\n%s", view)
	}
	if !strings.Contains(view, "space toggle \u00b7 enter continue \u00b7 esc back \u00b7 q cancel") {
		t.Fatalf("footer:\n%s", view)
	}
}

func TestFrameWizardPinsTheFooterAndDropsTheLeadingBlank(t *testing.T) {
	view := "\n  freecad-mcp setup\n\n  Title\n\n  content\n\n  space toggle \u00b7 enter continue"
	lines := plainLines(frameWizard(view, screenSize{80, 12}))
	if len(lines) != 12 || lines[0] != "  freecad-mcp setup" {
		t.Fatalf("%d rows, first %q", len(lines), lines[0])
	}
	if !strings.Contains(lines[11], "enter continue") {
		t.Fatalf("last row = %q", lines[11])
	}
	// A screen with no footer is only trimmed.
	if got := frameWizard("\n  a\n  b\n", screenSize{80, 12}); got != "  a\n  b" {
		t.Fatalf("no footer: %q", got)
	}
}

func TestFormKeysEditAFieldInPlace(t *testing.T) {
	p := newPalette()
	f := newForm(p,
		&control{id: "on", kind: kindToggle, label: "Sharing"},
		&control{id: "name", kind: kindText, label: "Name", input: newTextField(p, "none", false)},
		&control{id: "go", kind: kindAction, label: "Test connection"},
	)
	f.handleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if !f.control("on").on {
		t.Fatal("space did not turn the toggle on")
	}
	f.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if f.current().id != "name" {
		t.Fatalf("tab moved to %q", f.current().id)
	}
	for _, k := range []tea.KeyMsg{
		keyRunes("ab"), {Type: tea.KeyLeft}, keyRunes("X"), {Type: tea.KeyHome}, keyRunes("Z"),
		{Type: tea.KeyDelete}, {Type: tea.KeyEnd}, keyRunes("!"),
	} {
		f.handleKey(k)
	}
	if got := f.control("name").input.Value(); got != "ZXb!" {
		t.Fatalf("value = %q, want ZXb!", got)
	}
	f.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	f.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if f.current().id != "go" {
		t.Fatalf("shift+tab wrapped to %q", f.current().id)
	}
}

func TestFormMasksAPasswordAndShowsAPlaceholder(t *testing.T) {
	p := newPalette()
	secret := newTextField(p, "none", true)
	secret.SetValue("hunter2")
	empty := newTextField(p, "optional", false)
	f := newForm(p,
		&control{id: "pw", kind: kindText, label: "Password", input: secret},
		&control{id: "note", kind: kindText, label: "Note", input: empty, err: "Too long."},
	)
	view := stripANSI(strings.Join(f.view(80), "\n"))
	if strings.Contains(view, "hunter2") || !strings.Contains(view, "*******") {
		t.Fatalf("password not masked:\n%s", view)
	}
	if !strings.Contains(view, "optional") || !strings.Contains(view, "Too long.") {
		t.Fatalf("placeholder or error missing:\n%s", view)
	}
}

func TestSharePageShowsFieldErrorsUnderTheField(t *testing.T) {
	page := &sharePage{p: newPalette(), ctx: context.Background()}
	page.buildForm(addoninstall.DefaultRemoteSettings())
	page.form.control(shareMinutes).input.SetValue("0")
	if _, ok := page.options(); ok {
		t.Fatal("0 minutes accepted")
	}
	if page.form.control(shareMinutes).err == "" || page.form.current().id != shareMinutes {
		t.Fatalf("error %q, focus %q", page.form.control(shareMinutes).err, page.form.current().id)
	}
	page.form.control(shareMinutes).input.SetValue("30")
	page.form.control(sharePort).input.SetValue("70000")
	if _, ok := page.options(); ok {
		t.Fatal("port 70000 accepted")
	}
	if page.form.control(sharePort).hidden || page.form.current().id != sharePort || page.form.control(sharePort).err == "" {
		t.Fatalf("a hidden field's error must open Advanced and take the focus")
	}
	if page.form.control(shareMinutes).err != "" {
		t.Fatal("the fixed field still shows its error")
	}
}

func TestSharePageSavesSharingOffWithoutValidatingTheFields(t *testing.T) {
	page := &sharePage{p: newPalette(), ctx: context.Background()}
	page.buildForm(addoninstall.DefaultRemoteSettings())
	page.form.control(shareMinutes).input.SetValue("0")
	cmd := page.startSave()
	if cmd == nil || !page.saving || page.form.control(shareMinutes).err != "" {
		t.Fatalf("save with sharing off: cmd %v, saving %v", cmd != nil, page.saving)
	}
}

func TestSharePageFooterNamesOnlyWorkingKeys(t *testing.T) {
	page := &sharePage{p: newPalette(), ctx: context.Background()}
	page.buildForm(addoninstall.DefaultRemoteSettings())
	if got := page.footer(); got != "tab move \u00b7 space turn on \u00b7 enter save \u00b7 esc back" {
		t.Fatalf("toggle footer = %q", got)
	}
	page.form.move(1)
	if got := page.footer(); got != "tab move \u00b7 enter save \u00b7 esc back" {
		t.Fatalf("field footer = %q", got)
	}
	page.form.focusID(shareTest)
	if got := page.footer(); got != "tab move \u00b7 enter test \u00b7 esc back" {
		t.Fatalf("action footer = %q", got)
	}
}

func TestWizardConnectStepUsesFieldsAndShowsAnInlineError(t *testing.T) {
	step := &connectHostStep{ctx: context.Background()}
	state := &AppState{}
	state.Addon.Phase = addonNotFound
	step.Init(state)
	if d, _ := step.Update(tea.KeyMsg{Type: tea.KeyEnter}, state); d != flow.Continue || state.Connect.testing {
		t.Fatalf("enter with no host: %v, testing %v", d, state.Connect.testing)
	}
	if view := stripANSI(step.View(state)); !strings.Contains(view, "Enter a host first.") {
		t.Fatalf("no inline error:\n%s", view)
	}
	step.Update(keyRunes("pc1"), state)
	step.Update(tea.KeyMsg{Type: tea.KeyTab}, state)
	step.Update(keyRunes("98x"), state)
	if got := state.Connect.hostInput.Value() + ":" + state.Connect.portInput.Value(); got != "pc1:987698" {
		t.Fatalf("fields = %q", got)
	}
	if state.Connect.hostErr != "" {
		t.Fatal("typing did not clear the error")
	}
}
