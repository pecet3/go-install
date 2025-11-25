package main

import (
	"flag"
	"fmt"
	"go-installer/internal/cli"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	help := flag.Bool("h", false, "show help")
	flag.BoolVar(help, "help", false, "show help")
	version := flag.String("version", "", "Go version to install")
	flag.Parse()

	if *help {
		fmt.Println("usage: go-install [--version VERSION]")
		fmt.Println("example: go-install --version 1.22.1")
		fmt.Println("\nIf version is omitted, an interactive picker will be shown.")
		fmt.Println("\nNote: This tool requires root privileges (use sudo).")
		return
	}

	if os.Geteuid() != 0 {
		fmt.Println(cli.ErrorStyle.Render("\n✗ Error: This tool requires root privileges. Please run with sudo.\n"))
		os.Exit(1)
	}

	m := cli.NewMainModel(*version)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
