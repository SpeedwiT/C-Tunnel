// Package ui renders C Tunnel's terminal branding and (later) the interactive menu.
package ui

import (
	"fmt"
	"strings"
)

// ANSI colors (kept tiny; the installer/menu use them).
const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Cyan   = "\033[36m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Red    = "\033[31m"
	Dim    = "\033[2m"
)

const art = `
   ██████╗    ████████╗██╗   ██╗███╗   ██╗███╗   ██╗███████╗██╗
  ██╔════╝    ╚══██╔══╝██║   ██║████╗  ██║████╗  ██║██╔════╝██║
  ██║            ██║   ██║   ██║██╔██╗ ██║██╔██╗ ██║█████╗  ██║
  ██║            ██║   ██║   ██║██║╚██╗██║██║╚██╗██║██╔══╝  ██║
  ╚██████╗       ██║   ╚██████╔╝██║ ╚████║██║ ╚████║███████╗███████╗
   ╚═════╝       ╚═╝    ╚═════╝ ╚═╝  ╚═══╝╚═╝  ╚═══╝╚══════╝╚══════╝`

// Banner returns the C TUNNEL banner with the creator/github line.
func Banner(version, github, creator string) string {
	var b strings.Builder
	b.WriteString(Cyan + Bold + art + Reset + "\n")
	tag := "  anti-DPI · anti-filter · multi-transport tunnel"
	b.WriteString(Dim + tag + Reset + "\n")
	line := fmt.Sprintf("  %sversion%s %s   %sgithub%s %s   %sby%s %s",
		Dim, Reset, version, Dim, Reset, github, Dim, Reset, creator)
	b.WriteString(line + "\n")
	b.WriteString(Dim + "  ────────────────────────────────────────────────────────────" + Reset + "\n")
	return b.String()
}
