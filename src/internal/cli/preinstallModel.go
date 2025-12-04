package cli

import (
	"encoding/json"
	"fmt"
	"go-installer/common"
	"net/http"
	"os"
	"strings"

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

type preinstallState int

const (
	preinstallStateCheckingDeps preinstallState = iota
	preinstallStateConfirmInstallDeps
	preinstallStateInstallingDeps
	preinstallStateFetching
	preinstallStateSelectVersion
	preinstallStateConfirmOverride
	preinstallStateInstalling
	preinstallStateDone
	preinstallStateError
)

type preInstallModel struct {
	state       preinstallState
	releases    []common.GoRelease
	list        list.Model
	spinner     spinner.Model
	selectedVer string
	targetOS    string
	targetArch  string
	err         error

	missingDeps []dependency
	distro      distroInfo
}

func NewPreInstallModel(version string) preInstallModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return preInstallModel{
		state:       preinstallStateCheckingDeps,
		targetOS:    common.GetOS(),
		targetArch:  common.GetArch(),
		spinner:     s,
		selectedVer: common.NormalizeVersion(version),
	}
}

func (m preInstallModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		checkDependencies,
	)
}

func (m preInstallModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.state {
		case preinstallStateConfirmInstallDeps:
			switch msg.String() {
			case "y", "Y":
				m.state = preinstallStateInstallingDeps
				return m, tea.Batch(
					m.spinner.Tick,
					installDependencies(m.distro, m.missingDeps),
				)
			case "n", "N":
				m.state = preinstallStateError
				m.err = fmt.Errorf("dependencies are required for Go installation")
				return m, tea.Quit
			case "q", "ctrl+c":
				return m, tea.Quit
			}

		case preinstallStateSelectVersion:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "enter":
				i, ok := m.list.SelectedItem().(item)
				if ok {
					m.selectedVer = i.title
					if _, err := os.Stat("/usr/local/go"); err == nil {
						m.state = preinstallStateConfirmOverride
						return m, nil
					}
					return m.startInstallation()
				}
			}

		case preinstallStateConfirmOverride:
			switch msg.String() {
			case "y", "Y":
				return m.startInstallation()
			case "n", "N", "q", "ctrl+c":
				return m, tea.Quit
			}
		}

	case depsCheckMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = preinstallStateError
			return m, tea.Quit
		}

		m.distro = msg.distro

		if len(msg.missing) == 0 {
			// All dependencies satisfied, proceed to fetching releases
			m.state = preinstallStateFetching
			return m, tea.Batch(
				m.spinner.Tick,
				fetchReleases,
			)
		}

		// Some dependencies are missing
		m.missingDeps = msg.missing
		m.state = preinstallStateConfirmInstallDeps
		return m, nil

	case fetchedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = preinstallStateError
			return m, tea.Quit
		}

		m.releases = msg.releases

		if m.selectedVer != "" {
			if _, _, _, err := common.FindBuild(m.releases, m.selectedVer, m.targetOS, m.targetArch); err == nil {
				if _, err := os.Stat("/usr/local/go"); err == nil {
					m.state = preinstallStateConfirmOverride
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
		m.state = preinstallStateSelectVersion
		return m, nil

	case installCompleteMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = preinstallStateError
		} else {
			m.state = preinstallStateDone
		}
		return m, tea.Quit
	case depsInstallMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = preinstallStateError
			return m, tea.Quit
		}

		// Dependencies installed successfully, proceed to fetching releases
		m.state = preinstallStateFetching
		return m, tea.Batch(
			m.spinner.Tick,
			fetchReleases,
		)

	case spinner.TickMsg:
		if m.state == preinstallStateCheckingDeps ||
			m.state == preinstallStateInstallingDeps ||
			m.state == preinstallStateFetching {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	if m.state == preinstallStateSelectVersion {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m preInstallModel) View() string {
	switch m.state {
	case preinstallStateCheckingDeps:
		return fmt.Sprintf("\n%s Checking system dependencies...\n", m.spinner.View())

	case preinstallStateConfirmInstallDeps:
		var sb strings.Builder
		sb.WriteString(TitleStyle.Render("⚠️  Missing Dependencies") + "\n\n")
		sb.WriteString("The following dependencies are required:\n\n")

		for _, dep := range m.missingDeps {
			status := "recommended"
			if dep.required {
				status = "required"
			}
			sb.WriteString(fmt.Sprintf("  • %s (%s)\n", dep.name, status))
		}

		sb.WriteString("\nDetected system: ")
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render(m.distro.name))
		sb.WriteString(" (")
		sb.WriteString(m.distro.packageManager)
		sb.WriteString(")\n\n")

		// Show install command
		packages := make(map[string]bool)
		for _, dep := range m.missingDeps {
			if pkgName, ok := dep.packageName[m.distro.name]; ok {
				for _, pkg := range strings.Fields(pkgName) {
					packages[pkg] = true
				}
			}
		}
		var pkgList []string
		for pkg := range packages {
			pkgList = append(pkgList, pkg)
		}

		installCommand := fmt.Sprintf("sudo %s %s",
			m.distro.installCmd,
			strings.Join(pkgList, " "))

		sb.WriteString("Install command:\n")
		sb.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render(fmt.Sprintf("  %s", installCommand)))
		sb.WriteString("\n\n")
		sb.WriteString("Install dependencies now? (y/n): ")

		return sb.String()

	case preinstallStateInstallingDeps:
		return fmt.Sprintf("\n%s Installing dependencies...\n", m.spinner.View())

	case preinstallStateFetching:
		return fmt.Sprintf("\n%s Fetching Go releases metadata...\n", m.spinner.View())

	case preinstallStateSelectVersion:
		return "\n" + m.list.View()

	case preinstallStateConfirmOverride:
		return TitleStyle.Render("⚠️  /usr/local/go already exists. Override? (y/n): ")

	case preinstallStateInstalling:
		return "" // Install model handles its own view

	case preinstallStateError:
		return ErrorStyle.Render(fmt.Sprintf("\n✗ Error: %v\n\n", m.err))

	case preinstallStateDone:
		return SuccessStyle.Render(fmt.Sprintf("\n✓ Successfully installed %s to /usr/local/go\n\n", m.selectedVer))
	}

	return ""
}

func (m preInstallModel) startInstallation() (tea.Model, tea.Cmd) {
	m.state = preinstallStateInstalling
	installMod := newInstallModel(m.selectedVer, m.targetOS, m.targetArch, m.releases)
	return installMod, installMod.Init()
}
