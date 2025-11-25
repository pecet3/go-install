package cli

import (
	"encoding/json"
	"fmt"
	"go-installer/common"
	"net/http"
	"os"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func fetchReleases() tea.Msg {
	resp, err := http.Get("https://go.dev/dl/?mode=json&include=all")
	if err != nil {
		return fetchedMsg{err: err}
	}
	defer resp.Body.Close()

	var releases []common.GoRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return fetchedMsg{err: err}
	}

	return fetchedMsg{releases: releases}
}

type item struct {
	title, desc string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

type fetchedMsg struct {
	releases []common.GoRelease
	err      error
}

type installCompleteMsg struct {
	err error
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.state {
		case mainStateSelectVersion:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "enter":
				i, ok := m.list.SelectedItem().(item)
				if ok {
					m.selectedVer = i.title
					if _, err := os.Stat("/usr/local/go"); err == nil {
						m.state = mainStateConfirmOverride
						return m, nil
					}
					return m.startInstallation()
				}
			}

		case mainStateConfirmOverride:
			switch msg.String() {
			case "y", "Y":
				return m.startInstallation()
			case "n", "N", "q", "ctrl+c":
				return m, tea.Quit
			}

		case mainStateDone, mainStateError:
			return m, tea.Quit
		}

	case fetchedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = mainStateError
			return m, tea.Quit
		}

		m.releases = msg.releases

		if m.selectedVer != "" {
			if _, _, _, err := common.FindBuild(m.releases, m.selectedVer, m.targetOS, m.targetArch); err == nil {
				if _, err := os.Stat("/usr/local/go"); err == nil {
					m.state = mainStateConfirmOverride
					return m, nil
				}
				return m.startInstallation()
			}
		}

		items := make([]list.Item, 0, len(m.releases))
		for i, r := range m.releases {
			desc := "Go release"
			if i == 0 {
				desc = "Latest stable release"
			}
			items = append(items, item{title: r.Version, desc: desc})
		}

		l := list.New(items, list.NewDefaultDelegate(), 60, 14)
		l.Title = "Select Go Version"
		l.SetShowStatusBar(false)
		l.SetFilteringEnabled(true)
		l.Styles.Title = TitleStyle

		m.list = l
		m.state = mainStateSelectVersion
		return m, nil

	case installCompleteMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = mainStateError
		} else {
			m.state = mainStateDone
		}
		return m, tea.Quit

	case spinner.TickMsg:
		if m.state == mainStateFetching {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	if m.state == mainStateSelectVersion {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m mainModel) View() string {
	switch m.state {
	case mainStateFetching:
		return fmt.Sprintf("\n%s Fetching Go releases metadata...\n", m.spinner.View())

	case mainStateSelectVersion:
		return "\n" + m.list.View()

	case mainStateConfirmOverride:
		return TitleStyle.Render("⚠️  /usr/local/go already exists. Override? (y/n): ")

	case mainStateInstalling:
		return "" // Install model handles its own view

	case mainStateDone:
		return SuccessStyle.Render(fmt.Sprintf("\n✓ Successfully installed %s to /usr/local/go\n\n", m.selectedVer))

	case mainStateError:
		return ErrorStyle.Render(fmt.Sprintf("\n✗ Error: %v\n\n", m.err))
	}

	return ""
}

func (m mainModel) startInstallation() (tea.Model, tea.Cmd) {
	m.state = mainStateInstalling
	installMod := newInstallModel(m.selectedVer, m.targetOS, m.targetArch, m.releases)
	return installMod, installMod.Init()
}

type mainState int

const (
	mainStateFetching mainState = iota
	mainStateSelectVersion
	mainStateConfirmOverride
	mainStateInstalling
	mainStateDone
	mainStateError
)

type mainModel struct {
	state       mainState
	releases    []common.GoRelease
	list        list.Model
	spinner     spinner.Model
	selectedVer string
	targetOS    string
	targetArch  string
	err         error
	installProg *tea.Program
}

func NewMainModel(version string) mainModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return mainModel{
		state:       mainStateFetching,
		targetOS:    common.GetOS(),
		targetArch:  common.GetArch(),
		spinner:     s,
		selectedVer: common.NormalizeVersion(version),
	}
}

func (m mainModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		fetchReleases,
	)
}
