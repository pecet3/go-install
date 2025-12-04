package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go-installer/common"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func downloadFile(name string) error {
	out, err := os.Create(name)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := http.Get("https://go.dev/dl/" + name)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func verifyChecksum(name, want string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("sha mismatch: want=%s got=%s", want, got)
	}

	return nil
}

func setupEnvironment() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	shell := os.Getenv("SHELL")
	var configFiles []string

	if strings.Contains(shell, "zsh") {
		configFiles = []string{filepath.Join(homeDir, ".zshrc")}
	} else if strings.Contains(shell, "bash") {
		configFiles = []string{
			filepath.Join(homeDir, ".bashrc"),
			filepath.Join(homeDir, ".bash_profile"),
		}
	} else {
		configFiles = []string{filepath.Join(homeDir, ".bashrc")}
	}

	goPath := "export PATH=$PATH:/usr/local/go/bin"
	goPathComment := "# Added by go-install"

	for _, configFile := range configFiles {
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			continue
		}

		content, err := os.ReadFile(configFile)
		if err != nil {
			continue
		}

		if strings.Contains(string(content), "/usr/local/go/bin") {
			return nil
		}

		f, err := os.OpenFile(configFile, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			continue
		}
		defer f.Close()

		if _, err := f.WriteString(fmt.Sprintf("\n%s\n%s\n", goPathComment, goPath)); err != nil {
			continue
		}

		return nil
	}

	return fmt.Errorf("could not find shell config file to update")
}

func extractTarGz(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	t := tar.NewReader(gz)

	for {
		h, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dst, h.Name)

		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(h.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(h.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, t); err != nil {
				w.Close()
				return err
			}
			if err := w.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
	}

	return nil
}

type installState int

const (
	installStateDownloading installState = iota
	installStateVerifying
	installStateRemoving
	installStateExtracting
	installStateConfiguring
	installStateDone
	installStateError
)

// Osobne typy wiadomości dla każdego kroku
type downloadedMsg struct {
	filename string
	sha256   string
	err      error
}

type verifiedMsg struct {
	err error
}

type removedMsg struct {
	err error
}

type extractedMsg struct {
	err error
}

type configuredMsg struct {
	err error
}

type installModel struct {
	state      installState
	spinner    spinner.Model
	version    string
	targetOS   string
	targetArch string
	releases   []common.GoRelease
	err        error
	filename   string
	sha256     string
}

func newInstallModel(version, targetOS, targetArch string, releases []common.GoRelease) installModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return installModel{
		state:      installStateDownloading,
		spinner:    s,
		version:    version,
		targetOS:   targetOS,
		targetArch: targetArch,
		releases:   releases,
	}
}

func (m installModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}

	case downloadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = installStateError
			return m, tea.Quit
		}
		m.filename = msg.filename
		m.sha256 = msg.sha256
		m.state = installStateVerifying
		return m, m.stepVerify()

	case verifiedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = installStateError
			return m, tea.Quit
		}
		m.state = installStateRemoving
		return m, m.stepRemove()

	case removedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = installStateError
			return m, tea.Quit
		}
		m.state = installStateExtracting
		return m, m.stepExtract()

	case extractedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = installStateError
			return m, tea.Quit
		}
		m.state = installStateConfiguring
		return m, m.stepConfigure()

	case configuredMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = installStateError
			return m, tea.Quit
		}
		m.state = installStateDone

		return m, tea.Quit

	case spinner.TickMsg:
		if m.state == installStateDone || m.state == installStateError {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	}

	return m, nil
}

func (m installModel) View() string {
	if m.state == installStateError {
		return ErrorStyle.Render(fmt.Sprintf("\n✗ Error: %v\n\n", m.err))
	}
	if m.state == installStateDone {
		var sb strings.Builder
		sb.WriteString(SuccessStyle.Render(fmt.Sprintf("\n✓ Successfully installed %s to /usr/local/go")))
		sb.WriteString(InfoStyle.Render("\nPlease restart your terminal or run 'source' on your shell configuration file to apply the changes.\n"))
		return sb.String()
	}

	step := m.getStepDescription()
	return fmt.Sprintf("\n%s %s\n", m.spinner.View(), step)
}

func (m installModel) getStepDescription() string {
	switch m.state {
	case installStateDownloading:
		return "Downloading Go archive..."
	case installStateVerifying:
		return "Verifying checksum..."
	case installStateRemoving:
		return "Removing old installation..."
	case installStateExtracting:
		return "Extracting archive..."
	case installStateConfiguring:
		return "Configuring environment..."
	default:
		return "Installing..."
	}
}

func (m installModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.stepDownload(),
	)
}

func (m installModel) stepDownload() tea.Cmd {
	return func() tea.Msg {
		_, file, sha, err := common.FindBuild(m.releases, m.version, m.targetOS, m.targetArch)
		if err != nil {
			return downloadedMsg{err: err}
		}

		if err := downloadFile(file); err != nil {
			return downloadedMsg{err: err}
		}

		return downloadedMsg{
			filename: file,
			sha256:   sha,
			err:      nil,
		}
	}
}

func (m installModel) stepVerify() tea.Cmd {
	return func() tea.Msg {
		if err := verifyChecksum(m.filename, m.sha256); err != nil {
			return verifiedMsg{err: err}
		}
		return verifiedMsg{err: nil}
	}
}

func (m installModel) stepRemove() tea.Cmd {
	return func() tea.Msg {
		if err := os.RemoveAll("/usr/local/go"); err != nil {
			return removedMsg{err: err}
		}
		return removedMsg{err: nil}
	}
}

func (m installModel) stepExtract() tea.Cmd {
	return func() tea.Msg {
		if err := extractTarGz(m.filename, "/usr/local"); err != nil {
			return extractedMsg{err: err}
		}
		os.Remove(m.filename)
		return extractedMsg{err: nil}
	}
}

func (m installModel) stepConfigure() tea.Cmd {
	return func() tea.Msg {
		if err := setupEnvironment(); err != nil {
			return configuredMsg{err: err}
		}
		return configuredMsg{err: nil}
	}
}
