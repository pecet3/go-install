package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type dependency struct {
	name        string
	checkCmd    string
	packageName map[string]string
	required    bool
}

var requiredDeps = []dependency{
	{
		name:     "CA Certificates",
		checkCmd: "test -f /etc/ssl/certs/ca-certificates.crt",
		packageName: map[string]string{
			"debian": "ca-certificates",
			"ubuntu": "ca-certificates",
			"fedora": "ca-certificates",
			"rhel":   "ca-certificates",
			"arch":   "ca-certificates",
			"alpine": "ca-certificates",
		},
		required: true,
	},
	{
		name:     "GCC",
		checkCmd: "gcc --version",
		packageName: map[string]string{
			"debian": "build-essential",
			"ubuntu": "build-essential",
			"fedora": "gcc gcc-c++ make",
			"rhel":   "gcc gcc-c++ make",
			"arch":   "base-devel",
			"alpine": "build-base",
		},
		required: true,
	},
	{
		name:     "Make",
		checkCmd: "make --version",
		packageName: map[string]string{
			"debian": "build-essential",
			"ubuntu": "build-essential",
			"fedora": "make",
			"rhel":   "make",
			"arch":   "base-devel",
			"alpine": "build-base",
		},
		required: true,
	},
	{
		name:     "Git",
		checkCmd: "git --version",
		packageName: map[string]string{
			"debian": "git",
			"ubuntu": "git",
			"fedora": "git",
			"rhel":   "git",
			"arch":   "git",
			"alpine": "git",
		},
		required: false,
	},
}

type distroInfo struct {
	name           string
	packageManager string
	installCmd     string
	updateCmd      string
}

func detectDistro() distroInfo {
	// Check for package managers
	if _, err := exec.LookPath("apt-get"); err == nil {
		// Debian/Ubuntu
		data, _ := exec.Command("lsb_release", "-is").Output()
		distro := strings.ToLower(strings.TrimSpace(string(data)))
		if distro == "" {
			distro = "debian"
		}
		return distroInfo{
			name:           distro,
			packageManager: "apt-get",
			installCmd:     "apt-get install -y",
			updateCmd:      "apt-get update",
		}
	}

	if _, err := exec.LookPath("dnf"); err == nil {
		// Fedora/RHEL 8+
		return distroInfo{
			name:           "fedora",
			packageManager: "dnf",
			installCmd:     "dnf install -y",
			updateCmd:      "dnf check-update",
		}
	}

	if _, err := exec.LookPath("yum"); err == nil {
		// RHEL/CentOS 7
		return distroInfo{
			name:           "rhel",
			packageManager: "yum",
			installCmd:     "yum install -y",
			updateCmd:      "yum check-update",
		}
	}

	if _, err := exec.LookPath("pacman"); err == nil {
		// Arch Linux
		return distroInfo{
			name:           "arch",
			packageManager: "pacman",
			installCmd:     "pacman -S --noconfirm",
			updateCmd:      "pacman -Sy",
		}
	}

	if _, err := exec.LookPath("apk"); err == nil {
		// Alpine Linux
		return distroInfo{
			name:           "alpine",
			packageManager: "apk",
			installCmd:     "apk add",
			updateCmd:      "apk update",
		}
	}

	return distroInfo{
		name:           "unknown",
		packageManager: "unknown",
	}
}

type depsCheckMsg struct {
	missing []dependency
	distro  distroInfo
	err     error
}

type depsInstallMsg struct {
	err error
}

func checkDependencies() tea.Msg {
	distro := detectDistro()

	if distro.packageManager == "unknown" {
		return depsCheckMsg{
			err: fmt.Errorf("unable to detect package manager"),
		}
	}

	var missing []dependency
	for _, dep := range requiredDeps {
		parts := strings.Fields(dep.checkCmd)
		cmd := exec.Command(parts[0], parts[1:]...)
		if err := cmd.Run(); err != nil {
			missing = append(missing, dep)
		}
	}
	return depsCheckMsg{
		missing: missing,
		distro:  distro,
	}
}

func installDependencies(distro distroInfo, deps []dependency) tea.Cmd {
	return func() tea.Msg {
		// Update package lists
		if distro.updateCmd != "" {
			updateParts := strings.Fields(distro.updateCmd)
			updateCmd := exec.Command(updateParts[0], updateParts[1:]...)
			_ = updateCmd.Run() // Ignore errors for update
		}

		// Collect unique package names
		packages := make(map[string]bool)
		for _, dep := range deps {
			if pkgName, ok := dep.packageName[distro.name]; ok {
				for _, pkg := range strings.Fields(pkgName) {
					packages[pkg] = true
				}
			}
		}

		// Install packages
		var pkgList []string
		for pkg := range packages {
			pkgList = append(pkgList, pkg)
		}

		installParts := strings.Fields(distro.installCmd)
		installParts = append(installParts, pkgList...)
		installCmd := exec.Command(installParts[0], installParts[1:]...)

		if err := installCmd.Run(); err != nil {
			return depsInstallMsg{err: fmt.Errorf("failed to install packages: %w", err)}
		}

		return depsInstallMsg{}
	}
}

type depsState int

const (
	depsStateChecking depsState = iota
	depsStateConfirmInstall
	depsStateInstalling
	depsStateDone
	depsStateError
)

type depsModel struct {
	state   depsState
	spinner spinner.Model
	missing []dependency
	distro  distroInfo
	os      string
	err     error
}
