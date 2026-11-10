// Package ui renders C Tunnel's terminal branding and the interactive menu chrome.
package ui

import (
	"fmt"
	"strings"
)

// ANSI colors.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[97m"
	BCyan   = "\033[96m"
	BGreen  = "\033[92m"
	BMagen  = "\033[95m"
	BBlue   = "\033[94m"
)

// Clear wipes the screen and scrollback and homes the cursor, so each view starts on a clean frame.
func Clear() { fmt.Print("\033[2J\033[3J\033[H") }

const art = `   ██████╗    ████████╗██╗   ██╗███╗   ██╗███╗   ██╗███████╗██╗
  ██╔════╝    ╚══██╔══╝██║   ██║████╗  ██║████╗  ██║██╔════╝██║
  ██║            ██║   ██║   ██║██╔██╗ ██║██╔██╗ ██║█████╗  ██║
  ██║            ██║   ██║   ██║██║╚██╗██║██║╚██╗██║██╔══╝  ██║
  ╚██████╗       ██║   ╚██████╔╝██║ ╚████║██║ ╚████║███████╗███████╗
   ╚═════╝       ╚═╝    ╚═════╝ ╚═╝  ╚═══╝╚═╝  ╚═══╝╚══════╝╚══════╝`

const rule = "  ──────────────────────────────────────────────────────────────────"

// Banner returns the C TUNNEL banner with colored sub-lines.
func Banner(version, telegram, github, creator string) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(BCyan + Bold + art + Reset + "\n\n")
	b.WriteString("  " + BBlue + "fast" + Dim + " · " + Reset + BBlue + "anti-DPI" +
		Dim + " · " + Reset + BBlue + "anti-filter" + Dim + " · " + Reset + BBlue + "multi-transport tunnel" + Reset + "\n")
	line := fmt.Sprintf("  %sv%s%s    %sTelegram%s %s%s%s    %s·%s    %shttps://github.com/%s%s",
		BMagen+Bold, version, Reset,
		Dim, Reset, BGreen, telegram, Reset,
		Dim, Reset,
		BCyan, github, Reset)
	b.WriteString(line + "\n")
	b.WriteString(Dim + rule + Reset + "\n")
	return b.String()
}
