package main

// The interactive app opens when the binary is run bare in a terminal. It
// starts as a menu whose header carries the state that decides what to do next
// (FreeCAD running, sharing, the remote computer in use). Menu items open a
// page: the Doctor and Connection reports (app_report.go), the addon install
// plan (app_addon.go), and the Share this PC and Use another computer forms
// (app_share.go, app_connect.go). Every screen fills the terminal: header on
// the first row, footer on the last.

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sairaph/mcp-wizard/app"

	"github.com/sairaph/freecad-mcp/internal/addoninstall"
	"github.com/sairaph/freecad-mcp/internal/domain"
	"github.com/sairaph/freecad-mcp/internal/freecad"
)

// appPage is a page of the app. It gets every message while it is open (after
// ctrl+c, which always quits), starting with the terminal size, and reports
// done when the user leaves it (esc), which returns to the menu.
type appPage interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (done bool, cmd tea.Cmd)
	View() string
}

// What the menu header knows. Each part is unknown until its check answers.
type tri int

const (
	triUnknown tri = iota
	triYes
	triNo
)

type menuInfo struct {
	freecad tri
	sharing tri
	// host is the remote computer agents use, "" for this one.
	host string
}

// menuStateText is the state shown after the program name and version.
func menuStateText(info menuInfo) string {
	var parts []string
	switch info.freecad {
	case triYes:
		parts = append(parts, "FreeCAD running")
	case triNo:
		parts = append(parts, "FreeCAD not running")
	}
	switch info.sharing {
	case triYes:
		parts = append(parts, "sharing on")
	case triNo:
		parts = append(parts, "sharing off")
	}
	if info.host != "" {
		parts = append(parts, "using "+info.host)
	}
	return strings.Join(parts, " · ")
}

// menuHeaderText is the plain header of the menu.
func menuHeaderText(ver string, info menuInfo) string {
	header := domain.BinaryName + " " + ver
	if state := menuStateText(info); state != "" {
		header += "  " + state
	}
	return header
}

// The checks behind the header answer separately, so a slow one never holds
// back the others or the menu.
type (
	freecadStateMsg tri
	sharingStateMsg tri
	hostStateMsg    string
)

// readMenuInfo starts the checks behind the menu header.
func readMenuInfo(ctx context.Context) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			_, host, _, err := loadCredentials(ctx)
			if err != nil {
				return hostStateMsg("")
			}
			return hostStateMsg(host)
		},
		func() tea.Msg {
			settings, err := serverSettings(ctx)
			if err != nil {
				return freecadStateMsg(triUnknown)
			}
			conn := freecad.NewConnection(settings.Host, settings.Port, settings.Token, 3*time.Second)
			defer conn.Close()
			if ok, err := conn.Ping(ctx); err == nil && ok {
				return freecadStateMsg(triYes)
			}
			return freecadStateMsg(triNo)
		},
		func() tea.Msg {
			targets := addoninstall.LocateInstalled(ctx, freecadCommand())
			if len(targets) == 0 {
				return sharingStateMsg(triNo)
			}
			s, err := addoninstall.ReadRemoteSettings(targets[0])
			switch {
			case err != nil:
				return sharingStateMsg(triUnknown)
			case s.RemoteEnabled:
				return sharingStateMsg(triYes)
			}
			return sharingStateMsg(triNo)
		},
	)
}

type menuItem struct{ label, action string }

var menuItems = []menuItem{
	{"Doctor", "doctor"},
	{"Install addon", "addon"},
	{"Connection", "connection"},
	{"Share this PC", "share"},
	{"Use another computer", "connect"},
	{"Quit", "quit"},
}

type menuScreen struct {
	p      palette
	size   screenSize
	cursor int
	info   menuInfo
}

func (m *menuScreen) view() string {
	plain := menuHeaderText(version, m.info)
	name := domain.BinaryName + " " + version
	header := m.p.title.Render(name) + m.p.dim.Render(strings.TrimPrefix(plain, name))
	body := make([]string, 0, len(menuItems))
	for i, it := range menuItems {
		body = append(body, markRow(m.p, i == m.cursor, "", it.label, 0, ""))
	}
	footer := footerText(m.size.width(), hint{"↑↓", "move"}, hint{"enter", "select"}, hint{"q", "quit"})
	return renderScreenStyled(m.p, m.size, header, body, footer)
}

type appState struct {
	app.AppModel
	ctx  context.Context
	menu *menuScreen
	page appPage
}

func runApp(ctx context.Context) int {
	s := &appState{ctx: ctx, menu: &menuScreen{p: newPalette()}}
	return app.Run(ctx, s, app.Options{Title: domain.BinaryName, Version: version})
}

func (m *appState) Init() tea.Cmd { return readMenuInfo(m.ctx) }

func (m *appState) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := m.HandleGlobalKeys(msg); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.menu.size = screenSize{msg.Width, msg.Height}
	case freecadStateMsg:
		m.menu.info.freecad = tri(msg)
		return m, nil
	case sharingStateMsg:
		m.menu.info.sharing = tri(msg)
		return m, nil
	case hostStateMsg:
		m.menu.info.host = string(msg)
		return m, nil
	}

	if m.page != nil {
		done, cmd := m.page.Update(msg)
		if done {
			m.page = nil
			return m, tea.Batch(cmd, readMenuInfo(m.ctx))
		}
		return m, cmd
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.menu.cursor > 0 {
			m.menu.cursor--
		}
	case "down", "j":
		if m.menu.cursor < len(menuItems)-1 {
			m.menu.cursor++
		}
	case "q":
		m.Quit = true
		return m, tea.Quit
	case "enter":
		switch menuItems[m.menu.cursor].action {
		case "quit":
			m.Quit = true
			return m, tea.Quit
		case "doctor":
			return m.openPage(doctorPage(m.ctx))
		case "addon":
			return m.openPage(newAddonPage(m.ctx))
		case "connection":
			return m.openPage(connectionPage(m.ctx))
		case "share":
			return m.openPage(newSharePage(m.ctx))
		case "connect":
			return m.openPage(newConnectPage(m.ctx))
		}
	}
	return m, nil
}

// openPage shows page until it reports done, telling it the terminal size
// first.
func (m *appState) openPage(page appPage) (tea.Model, tea.Cmd) {
	m.page = page
	page.Update(tea.WindowSizeMsg{Width: m.menu.size.w, Height: m.menu.size.h})
	return m, page.Init()
}

func (m *appState) View() string {
	if m.page != nil {
		return m.page.View()
	}
	return m.menu.view()
}
