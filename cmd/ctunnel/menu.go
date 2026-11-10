package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	"github.com/SpeedwiT/c-tunnel/internal/linktest"
	"github.com/SpeedwiT/c-tunnel/internal/setupcode"
	"github.com/SpeedwiT/c-tunnel/internal/transport"
	"github.com/SpeedwiT/c-tunnel/internal/ui"
)

var stdin = bufio.NewReader(os.Stdin)

func ask(prompt, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", prompt, def)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	line, err := stdin.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		fmt.Println("\nNo terminal input — run `ctunnel` directly in a shell to use the menu.")
		os.Exit(0)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// header clears the screen and prints the banner so each view is a clean, full frame.
func header() {
	ui.Clear()
	fmt.Print(ui.Banner(version, telegram, github, creator))
}

// pause waits for Enter before returning to the menu.
func pause() {
	fmt.Printf("\n%sPress Enter to return to the menu…%s", ui.Dim, ui.Reset)
	_, _ = stdin.ReadString('\n')
}

// item renders one aligned menu row: number, bold title, dim hint.
func item(n, title, hint string) {
	fmt.Printf("  %s%2s%s) %s%-18s%s %s%s%s\n",
		ui.BCyan, n, ui.Reset, ui.Bold, title, ui.Reset, ui.Dim, hint, ui.Reset)
}

// runMenu is the interactive terminal entrypoint (ctunnel with no args).
func runMenu() {
	for {
		header()
		fmt.Println()
		item("0", "Connection Test", "which transports work between the two servers")
		item("1", "Setup Iran", "the server users connect to")
		item("2", "Setup Foreign", "the server abroad, with the service")
		item("3", "Manage", "status, health, Link Test, change transport")
		item("4", "Backup & Restore", "all settings in one file")
		item("5", "Optimize", "BBR, buffers, limits")
		item("6", "Telegram Bot", "reports and alerts")
		item("7", "Update", "fetch the latest build (with rollback)")
		item("8", "Uninstall", "remove everything")
		item("9", "Exit", "")
		fmt.Println()
		switch ask("Select an option", "") {
		case "0":
			connectionTest()
		case "1":
			screen(wizardRelay)
		case "2":
			screen(wizardExit)
		case "3":
			manageMenu()
		case "4":
			backupMenu()
		case "5":
			screen(optimize)
		case "6":
			screen(configureTelegram)
		case "7":
			screen(updateBinary)
		case "8":
			screen(uninstall)
		case "9", "q", "exit":
			ui.Clear()
			return
		}
	}
}

// screen runs fn on a fresh frame and pauses before returning to the menu.
func screen(fn func()) {
	header()
	fmt.Println()
	fn()
	pause()
}

// chooseTransport walks the user through category -> transport.
func chooseTransport() string {
	grouped := transport.Grouped()
	fmt.Printf("%sChoose a transport category:%s\n", ui.Bold, ui.Reset)
	for i, cat := range transport.CategoryOrder {
		fmt.Printf("  %s%d%s) %s\n", ui.BCyan, i+1, ui.Reset, transport.CategoryTitle[cat])
	}
	ci, _ := strconv.Atoi(ask("Category", "1"))
	if ci < 1 || ci > len(transport.CategoryOrder) {
		ci = 1
	}
	list := grouped[transport.CategoryOrder[ci-1]]
	fmt.Println()
	for i, t := range list {
		fmt.Printf("  %s%d%s) %-8s %s%s%s\n", ui.BCyan, i+1, ui.Reset, t.Name(), ui.Dim, t.Description(), ui.Reset)
	}
	ti, _ := strconv.Atoi(ask("Transport", "1"))
	if ti < 1 || ti > len(list) {
		ti = 1
	}
	return list[ti-1].Name()
}

func chooseMode() string {
	fmt.Printf("%sTunnel mode:%s\n", ui.Bold, ui.Reset)
	fmt.Printf("  %s1%s) Reverse — the foreign server dials in to Iran; Iran needs one open port\n", ui.BCyan, ui.Reset)
	fmt.Printf("  %s2%s) Direct  — Iran dials out; no inbound port on Iran (full IP tunnel)\n", ui.BCyan, ui.Reset)
	if ask("Mode", "1") == "2" {
		return config.ModeDirect
	}
	return config.ModeReverse
}

func askForwards() []config.Forward {
	var fwds []config.Forward
	fmt.Printf("\n%sForwarded ports%s (leave the name empty to finish)\n", ui.Bold, ui.Reset)
	for {
		name := ask("Forward name", "")
		if name == "" {
			break
		}
		lp, _ := strconv.Atoi(ask("Listen port (on the Iran server)", "443"))
		target := ask("Target on the foreign server (host:port)", "127.0.0.1:443")
		fwds = append(fwds, config.Forward{Name: name, Listen: lp, Target: target, Proto: "tcp"})
	}
	return fwds
}

func wizardRelay() {
	fmt.Printf("%s== Setup Iran (Relay) ==%s\n\n", ui.BGreen, ui.Reset)
	mode := chooseMode()
	var tr string
	var members []string
	if ask("Bond several transports at once? (cbond) (y/n)", "n") == "y" {
		tr = "cbond"
		list := ask("Member transports (comma-separated)", "cquic,ctls,ckcp")
		for _, m := range strings.Split(list, ",") {
			if m = strings.TrimSpace(m); m != "" {
				members = append(members, m)
			}
		}
	} else {
		tr = chooseTransport()
	}
	listenPort := ask("Tunnel port on Iran", "7000")
	cfg := &config.Config{
		Role: config.RoleRelay, Mode: mode, Transport: tr, Members: members,
		Listen: ":" + listenPort, PSK: config.GenPSK(),
		SNI:      ask("SNI (decoy TLS domain)", "www.cloudflare.com"),
		Forwards: askForwards(), Creator: creator, GitHub: github,
	}
	var advAddr string
	if mode == config.ModeReverse {
		advAddr = ask("Public IP of this Iran server", "") + ":" + listenPort
	} else {
		fip := ask("Foreign server IP (Iran dials it)", "")
		fport := ask("Foreign server port", listenPort)
		cfg.Peer = fip + ":" + fport
		cfg.TunSubnet = ask("Private tunnel subnet (TUN /30)", "10.72.0.1/30")
		advAddr = "0.0.0.0:" + fport
	}
	if tr == "crdv" {
		cfg.Broker = ask("Broker address (host:port)", cfg.Broker)
	}
	if tr == "cspoof" {
		cfg.SpoofSrc = ask("Forged source IP to send from", "")
	}
	if err := cfg.Validate(); err != nil {
		fmt.Println(ui.Red+"Error:"+ui.Reset, err)
		return
	}
	saveAndMaybeService(cfg)
	setup := setupcode.FromRelay(cfg, advAddr)
	fmt.Printf("\n%s✓ Iran server ready.%s Run the setup code on the foreign server:\n", ui.BGreen, ui.Reset)
	if cfg.Broker != "" && ask("Mint a short code? (needs the broker) (y/n)", "y") == "y" {
		if code, err := setupcode.MintShort(cfg.Broker, setup); err != nil {
			fmt.Println(ui.Yellow+"Short code failed:"+ui.Reset, err)
		} else {
			fmt.Printf("  %s%s%s   (valid 30 minutes)\n", ui.BGreen, code, ui.Reset)
			fmt.Printf("  On the foreign server: %sctunnel join %s -broker %s%s\n", ui.BCyan, code, cfg.Broker, ui.Reset)
			return
		}
	}
	fmt.Printf("  %sctunnel join %s%s\n", ui.BCyan, setupcode.Encode(setup), ui.Reset)
}

func wizardExit() {
	fmt.Printf("%s== Setup Foreign (Exit) ==%s\n\n", ui.BGreen, ui.Reset)
	code := ask("Paste the setup code", "")
	if code == "" {
		fmt.Println(ui.Yellow + "No code entered." + ui.Reset)
		return
	}
	var s setupcode.Setup
	var err error
	if setupcode.IsShort(code) {
		s, err = setupcode.ResolveShort(ask("Broker address (host:port)", ""), code)
	} else {
		s, err = setupcode.Decode(code)
	}
	if err != nil {
		fmt.Println(ui.Red+"Invalid code:"+ui.Reset, err)
		return
	}
	cfg := s.ExitConfig()
	cfg.Creator, cfg.GitHub = creator, github
	if err := cfg.Validate(); err != nil {
		fmt.Println(ui.Red+"Error:"+ui.Reset, err)
		return
	}
	saveAndMaybeService(cfg)
	fmt.Printf("%s✓ Foreign server joined the tunnel%s (mode=%s transport=%s)\n", ui.BGreen, ui.Reset, cfg.Mode, cfg.Transport)
}

// connectionTest probes which transports connect between the two servers (menu-friendly, returns).
func connectionTest() {
	header()
	fmt.Printf("\n%s== Connection Test ==%s\n\n", ui.BGreen, ui.Reset)
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		fmt.Println(ui.Yellow+"No config yet — set up a server first."+ui.Reset, "("+err.Error()+")")
		pause()
		return
	}
	names := transport.Names()
	if cfg.Role == config.RoleRelay {
		stop, err := linktest.Server(cfg, names, linkTestBasePort)
		if err != nil {
			fmt.Println(ui.Red+"Could not start probe server:"+ui.Reset, err)
			pause()
			return
		}
		defer stop()
		fmt.Printf("%sProbe server running%s on ports %d..%d.\n", ui.BGreen, ui.Reset, linkTestBasePort, linkTestBasePort+len(names)-1)
		fmt.Printf("On the foreign server run:  %sctunnel linktest -host <this-iran-ip>%s\n", ui.BCyan, ui.Reset)
		pause()
		return
	}
	host := hostOf(cfg.Peer)
	fmt.Printf("Testing %d transports against %s …\n", len(names), host)
	printResults(linktest.ProbeAll(cfg, names, host, linkTestBasePort, 6*time.Second))
	pause()
}

func manageMenu() {
	for {
		header()
		fmt.Printf("\n  %s== Manage ==%s\n\n", ui.BGreen, ui.Reset)
		item("1", "Status & live ping", "")
		item("2", "Link Test", "probe every transport")
		item("3", "Change transport", "no reinstall")
		item("4", "Restart service", "")
		item("5", "Service logs", "last 30 lines")
		item("0", "Back", "")
		fmt.Println()
		switch ask("Select an option", "") {
		case "1":
			header()
			fmt.Println()
			cmdStatus(nil)
			pause()
		case "2":
			connectionTest()
		case "3":
			header()
			fmt.Println()
			cmdSwitch([]string{chooseTransport()})
			pause()
		case "4":
			screen(func() { serviceCmd("restart") })
		case "5":
			screen(serviceLogs)
		case "0", "q":
			return
		}
	}
}

func serviceLogs() {
	fmt.Printf("%s== Service logs ==%s\n\n", ui.BGreen, ui.Reset)
	if _, err := exec.LookPath("journalctl"); err != nil {
		fmt.Println(ui.Yellow + "journalctl not available." + ui.Reset)
		return
	}
	out, _ := exec.Command("journalctl", "-u", "ctunnel", "-n", "30", "--no-pager").CombinedOutput()
	fmt.Print(string(out))
}

func backupMenu() {
	for {
		header()
		fmt.Printf("\n  %s== Backup & Restore ==%s\n\n", ui.BGreen, ui.Reset)
		item("1", "Backup", "save all settings to one file")
		item("2", "Restore", "load settings from a file")
		item("0", "Back", "")
		fmt.Println()
		switch ask("Select an option", "") {
		case "1":
			screen(backupConfig)
		case "2":
			screen(restoreConfig)
		case "0", "q":
			return
		}
	}
}

func backupConfig() {
	fmt.Printf("%s== Backup ==%s\n\n", ui.BGreen, ui.Reset)
	data, err := os.ReadFile(config.DefaultPath())
	if err != nil {
		fmt.Println(ui.Red+"No config to back up:"+ui.Reset, err)
		return
	}
	dst := ask("Save backup to", "/root/ctunnel-backup.json")
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		fmt.Println(ui.Red+"Backup failed:"+ui.Reset, err)
		return
	}
	fmt.Printf("%s✓ Backup saved:%s %s\n", ui.BGreen, ui.Reset, dst)
}

func restoreConfig() {
	fmt.Printf("%s== Restore ==%s\n\n", ui.BGreen, ui.Reset)
	src := ask("Restore backup from", "/root/ctunnel-backup.json")
	data, err := os.ReadFile(src)
	if err != nil {
		fmt.Println(ui.Red+"Cannot read backup:"+ui.Reset, err)
		return
	}
	if err := os.WriteFile(config.DefaultPath(), data, 0o600); err != nil {
		fmt.Println(ui.Red+"Restore failed:"+ui.Reset, err)
		return
	}
	fmt.Printf("%s✓ Settings restored.%s Restart the service to apply.\n", ui.BGreen, ui.Reset)
	if ask("Restart now? (y/n)", "y") == "y" {
		serviceCmd("restart")
	}
}

// optimize applies BBR + sane network buffers for a fast tunnel.
func optimize() {
	fmt.Printf("%s== Optimize (BBR + buffers) ==%s\n\n", ui.BGreen, ui.Reset)
	if os.Geteuid() != 0 {
		fmt.Println(ui.Yellow + "Run as root to apply system tuning." + ui.Reset)
		return
	}
	const conf = `# C Tunnel network tuning
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.core.rmem_max=67108864
net.core.wmem_max=67108864
net.ipv4.tcp_rmem=4096 87380 67108864
net.ipv4.tcp_wmem=4096 65536 67108864
net.ipv4.tcp_mtu_probing=1
net.ipv4.udp_rmem_min=8192
net.ipv4.udp_wmem_min=8192
net.ipv4.ip_forward=1
`
	const path = "/etc/sysctl.d/99-ctunnel.conf"
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		fmt.Println(ui.Red+"Write failed:"+ui.Reset, err)
		return
	}
	out, err := exec.Command("sysctl", "--system").CombinedOutput()
	if err != nil {
		fmt.Printf("%ssysctl:%s %v\n%s\n", ui.Yellow, ui.Reset, err, out)
		return
	}
	cc, _ := exec.Command("sysctl", "-n", "net.ipv4.tcp_congestion_control").Output()
	fmt.Printf("%s✓ Applied.%s Congestion control: %s%s%s\n",
		ui.BGreen, ui.Reset, ui.Bold, strings.TrimSpace(string(cc)), ui.Reset)
}

// updateBinary downloads the latest prebuilt binary for this arch and swaps it in, with rollback.
func updateBinary() {
	fmt.Printf("%s== Update ==%s\n\n", ui.BGreen, ui.Reset)
	if os.Geteuid() != 0 {
		fmt.Println(ui.Yellow + "Run as root to update." + ui.Reset)
		return
	}
	arch := runtime.GOARCH // amd64 / arm64
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/C-Tunnel/main/dist/ctunnel-linux-%s", github, arch)
	self, err := os.Executable()
	if err != nil {
		self = "/usr/local/bin/ctunnel"
	}
	tmp := self + ".new"
	fmt.Printf("Downloading %s …\n", url)
	if out, err := exec.Command("curl", "-fsSL", url, "-o", tmp).CombinedOutput(); err != nil {
		fmt.Printf("%sDownload failed:%s %v\n%s\n", ui.Red, ui.Reset, err, out)
		return
	}
	_ = os.Chmod(tmp, 0o755)
	_ = os.Rename(self, self+".bak") // keep a rollback copy
	if err := os.Rename(tmp, self); err != nil {
		_ = os.Rename(self+".bak", self)
		fmt.Println(ui.Red+"Swap failed, rolled back:"+ui.Reset, err)
		return
	}
	// Verify the new binary runs; roll back if not.
	if err := exec.Command(self, "version").Run(); err != nil {
		_ = os.Rename(self+".bak", self)
		fmt.Println(ui.Red + "New binary failed, rolled back." + ui.Reset)
		return
	}
	fmt.Printf("%s✓ Updated.%s Restarting service…\n", ui.BGreen, ui.Reset)
	serviceCmd("restart")
}

func uninstall() {
	fmt.Printf("%s== Uninstall ==%s\n\n", ui.Red, ui.Reset)
	if ask("Remove the service and binary? (yes/no)", "no") != "yes" {
		fmt.Println("Cancelled.")
		return
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		_ = exec.Command("systemctl", "disable", "--now", "ctunnel").Run()
		_ = os.Remove("/etc/systemd/system/ctunnel.service")
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}
	_ = os.Remove("/usr/local/bin/ctunnel")
	if ask("Also delete the config at "+config.DefaultPath()+"? (y/n)", "n") == "y" {
		_ = os.Remove(config.DefaultPath())
	}
	fmt.Printf("%s✓ Uninstalled.%s\n", ui.BGreen, ui.Reset)
}

// configureTelegram enables/updates Telegram status notifications in the saved config.
func configureTelegram() {
	fmt.Printf("%s== Telegram Bot ==%s\n\n", ui.BGreen, ui.Reset)
	path := config.DefaultPath()
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Println(ui.Yellow+"Set up a server first."+ui.Reset, "("+err.Error()+")")
		return
	}
	if cfg.Telegram == nil {
		cfg.Telegram = &config.Telegram{}
	}
	fmt.Println("Create a bot with @BotFather, get the token, and enter your chat id.")
	if ask("Enable? (y/n)", map[bool]string{true: "y", false: "n"}[cfg.Telegram.Enabled]) != "y" {
		cfg.Telegram.Enabled = false
		_ = cfg.Save(path)
		fmt.Println(ui.Yellow + "Telegram notifications disabled." + ui.Reset)
		return
	}
	cfg.Telegram.Enabled = true
	cfg.Telegram.Token = ask("Bot token", cfg.Telegram.Token)
	cfg.Telegram.ChatID = ask("Chat id", cfg.Telegram.ChatID)
	iv, _ := strconv.Atoi(ask("Status interval (seconds)", nonZero(cfg.Telegram.Interval, 300)))
	cfg.Telegram.Interval = iv
	if err := cfg.Save(path); err != nil {
		fmt.Println(ui.Red+"Save failed:"+ui.Reset, err)
		return
	}
	fmt.Printf("%s✓ Telegram notifications enabled%s — restart the service to apply.\n", ui.BGreen, ui.Reset)
}

func nonZero(v, def int) string {
	if v == 0 {
		return strconv.Itoa(def)
	}
	return strconv.Itoa(v)
}

// saveAndMaybeService writes the config and offers to install+start the systemd service.
func saveAndMaybeService(cfg *config.Config) {
	path := config.DefaultPath()
	if err := cfg.Save(path); err != nil {
		fmt.Println(ui.Red+"Could not save config:"+ui.Reset, err)
		return
	}
	fmt.Printf("%s✓ Config saved:%s %s\n", ui.BGreen, ui.Reset, path)
	if ask("Install and start the systemd service? (y/n)", "y") == "y" {
		if err := installService(path); err != nil {
			fmt.Println(ui.Yellow+"Service install failed:"+ui.Reset, err)
			fmt.Printf("You can run it manually: %sctunnel run%s\n", ui.BCyan, ui.Reset)
		}
	}
}

const serviceUnit = `[Unit]
Description=C Tunnel (%s)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=%s run -config %s
Restart=always
RestartSec=2
LimitNOFILE=1048576
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=false

[Install]
WantedBy=multi-user.target
`

func installService(cfgPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	unit := fmt.Sprintf(serviceUnit, github, exe, cfgPath)
	const dst = "/etc/systemd/system/ctunnel.service"
	if err := os.WriteFile(dst, []byte(unit), 0o644); err != nil {
		return err
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl not found; run manually: %s run", exe)
	}
	for _, a := range [][]string{{"daemon-reload"}, {"enable", "--now", "ctunnel"}} {
		if out, err := exec.Command("systemctl", a...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %v: %s", strings.Join(a, " "), err, out)
		}
	}
	fmt.Printf("%s✓ ctunnel service is running%s (systemctl status ctunnel)\n", ui.BGreen, ui.Reset)
	return nil
}

func serviceCmd(action string) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		fmt.Println(ui.Yellow + "systemctl is not available." + ui.Reset)
		return
	}
	out, err := exec.Command("systemctl", action, "ctunnel").CombinedOutput()
	if err != nil {
		fmt.Printf("%s%s%s: %v\n%s\n", ui.Red, action, ui.Reset, err, out)
		return
	}
	fmt.Printf("%s✓ %s done%s\n", ui.BGreen, action, ui.Reset)
}
