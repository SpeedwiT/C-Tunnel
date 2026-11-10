package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/SpeedwiT/c-tunnel/internal/config"
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
		// No interactive input (EOF — e.g. run through a pipe). Exit cleanly instead of looping.
		fmt.Println("\nNo terminal input — run `ctunnel` directly in a shell to use the menu.")
		os.Exit(0)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// runMenu is the interactive terminal entrypoint (ctunnel with no args).
func runMenu() {
	fmt.Print(ui.Banner(version, github, creator))
	for {
		fmt.Printf(`
  %s1%s) Install Iran server (Relay)
  %s2%s) Install Foreign server (Exit) — with a setup code
  %s3%s) Manage: status, Link Test, change transport
  %s4%s) List transports
  %s5%s) Quit
`, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset)
		switch ask("Choice", "") {
		case "1":
			wizardRelay()
		case "2":
			wizardExit()
		case "3":
			manageMenu()
		case "4":
			printTransports()
		case "5", "q", "exit":
			return
		}
	}
}

// chooseTransport walks the user through category -> transport.
func chooseTransport() string {
	grouped := transport.Grouped()
	fmt.Printf("\n%sChoose a transport category:%s\n", ui.Bold, ui.Reset)
	for i, cat := range transport.CategoryOrder {
		fmt.Printf("  %s%d%s) %s\n", ui.Cyan, i+1, ui.Reset, transport.CategoryTitle[cat])
	}
	ci, _ := strconv.Atoi(ask("Category", "1"))
	if ci < 1 || ci > len(transport.CategoryOrder) {
		ci = 1
	}
	cat := transport.CategoryOrder[ci-1]
	list := grouped[cat]
	fmt.Println()
	for i, t := range list {
		fmt.Printf("  %s%d%s) %-8s %s\n", ui.Cyan, i+1, ui.Reset, t.Name(), t.Description())
	}
	ti, _ := strconv.Atoi(ask("Transport", "1"))
	if ti < 1 || ti > len(list) {
		ti = 1
	}
	return list[ti-1].Name()
}

func chooseMode() string {
	fmt.Printf("\n%sTunnel mode:%s\n", ui.Bold, ui.Reset)
	fmt.Printf("  %s1%s) Reverse — the foreign server dials in to Iran; Iran needs one open port\n", ui.Cyan, ui.Reset)
	fmt.Printf("  %s2%s) Direct  — Iran dials out; no inbound port on Iran (full IP tunnel)\n", ui.Cyan, ui.Reset)
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
	fmt.Printf("\n%s-- Install Iran server --%s\n", ui.Bold, ui.Reset)
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
		Role:      config.RoleRelay,
		Mode:      mode,
		Transport: tr,
		Members:   members,
		Listen:    ":" + listenPort,
		PSK:       config.GenPSK(),
		SNI:       ask("SNI (decoy TLS domain)", "www.cloudflare.com"),
		Forwards:  askForwards(),
		Creator:   creator,
		GitHub:    github,
	}
	var advAddr string
	if mode == config.ModeReverse {
		ip := ask("Public IP of this Iran server", "")
		advAddr = ip + ":" + listenPort
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
	fmt.Printf("\n%s✓ Iran server ready.%s Run the setup code on the foreign server:\n", ui.Green, ui.Reset)
	// Prefer a short code when a broker is available.
	if cfg.Broker != "" && ask("Mint a short code? (needs the broker) (y/n)", "y") == "y" {
		code, err := setupcode.MintShort(cfg.Broker, setup)
		if err != nil {
			fmt.Println(ui.Yellow+"Short code failed:"+ui.Reset, err)
		} else {
			fmt.Printf("  %s%s%s   (valid 30 minutes)\n", ui.Green, code, ui.Reset)
			fmt.Printf("  On the foreign server: %sctunnel join %s -broker %s%s\n", ui.Cyan, code, cfg.Broker, ui.Reset)
			return
		}
	}
	code := setupcode.Encode(setup)
	fmt.Printf("  %sctunnel join %s%s\n", ui.Cyan, code, ui.Reset)
}

func wizardExit() {
	fmt.Printf("\n%s-- Install Foreign server --%s\n", ui.Bold, ui.Reset)
	code := ask("Paste the setup code", "")
	if code == "" {
		fmt.Println(ui.Yellow + "No code entered." + ui.Reset)
		return
	}
	var s setupcode.Setup
	var err error
	if setupcode.IsShort(code) {
		b := ask("Broker address (host:port)", "")
		s, err = setupcode.ResolveShort(b, code)
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
	fmt.Printf("%s✓ Foreign server joined the tunnel%s (mode=%s transport=%s)\n", ui.Green, ui.Reset, cfg.Mode, cfg.Transport)
}

func manageMenu() {
	for {
		fmt.Printf(`
  %sManage%s
  %s1%s) Status and live ping
  %s2%s) Link Test (probe every transport)
  %s3%s) Change transport
  %s4%s) Telegram notifications (status/ping)
  %s5%s) Restart service
  %s6%s) Back
`, ui.Bold, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset)
		switch ask("Choice", "") {
		case "1":
			cmdStatus(nil)
		case "2":
			manageLinkTest()
		case "3":
			name := chooseTransport()
			cmdSwitch([]string{name})
		case "4":
			configureTelegram()
		case "5":
			serviceCmd("restart")
		case "6", "q":
			return
		}
	}
}

func manageLinkTest() {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		fmt.Println(ui.Red+"Could not read config:"+ui.Reset, err)
		return
	}
	if cfg.Role == config.RoleRelay {
		fmt.Println("On Iran: starting the probe server. On the foreign server run: ctunnel linktest -host <iran-ip>")
		cmdLinktestServer(nil)
		return
	}
	cmdLinktest(nil)
}

// configureTelegram enables/updates Telegram status notifications in the saved config.
func configureTelegram() {
	path := config.DefaultPath()
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Println(ui.Red+"Could not read config:"+ui.Reset, err)
		return
	}
	if cfg.Telegram == nil {
		cfg.Telegram = &config.Telegram{}
	}
	fmt.Printf("\n%s-- Telegram notifications --%s\n", ui.Bold, ui.Reset)
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
	fmt.Printf("%s✓ Telegram notifications enabled%s — restart the service to apply.\n", ui.Green, ui.Reset)
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
	fmt.Printf("%s✓ Config saved:%s %s\n", ui.Green, ui.Reset, path)
	if ask("Install and start the systemd service? (y/n)", "y") == "y" {
		if err := installService(path); err != nil {
			fmt.Println(ui.Yellow+"Service install failed:"+ui.Reset, err)
			fmt.Printf("You can run it manually: %sctunnel run%s\n", ui.Cyan, ui.Reset)
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
	fmt.Printf("%s✓ ctunnel service is running%s (systemctl status ctunnel)\n", ui.Green, ui.Reset)
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
	fmt.Printf("%s✓ %s done%s\n", ui.Green, action, ui.Reset)
}
