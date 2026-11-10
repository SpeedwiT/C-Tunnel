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
	line, _ := stdin.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// menu is the interactive terminal entrypoint (ctunnel with no args).
func runMenu() {
	fmt.Print(ui.Banner(version, github, creator))
	for {
		fmt.Printf(`
  %s۱%s) نصب سرور ایران (Relay)
  %s۲%s) نصب سرور خارج (Exit) — با کد ستاپ
  %s۳%s) مدیریت: وضعیت، Link Test، تغییر ترنسپورت
  %s۴%s) لیست ترنسپورت‌ها
  %s۵%s) خروج
`, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset)
		switch ask("انتخاب", "") {
		case "1", "۱":
			wizardRelay()
		case "2", "۲":
			wizardExit()
		case "3", "۳":
			manageMenu()
		case "4", "۴":
			printTransports()
		case "5", "۵", "q", "exit":
			return
		}
	}
}

// chooseTransport walks the user through category → transport.
func chooseTransport() string {
	grouped := transport.Grouped()
	fmt.Printf("\n%sنوع ترنسپورت را انتخاب کن:%s\n", ui.Bold, ui.Reset)
	for i, cat := range transport.CategoryOrder {
		fmt.Printf("  %s%d%s) %s\n", ui.Cyan, i+1, ui.Reset, transport.CategoryTitle[cat])
	}
	ci, _ := strconv.Atoi(ask("دسته", "1"))
	if ci < 1 || ci > len(transport.CategoryOrder) {
		ci = 1
	}
	cat := transport.CategoryOrder[ci-1]
	list := grouped[cat]
	fmt.Println()
	for i, t := range list {
		fmt.Printf("  %s%d%s) %-8s %s\n", ui.Cyan, i+1, ui.Reset, t.Name(), t.Description())
	}
	ti, _ := strconv.Atoi(ask("ترنسپورت", "1"))
	if ti < 1 || ti > len(list) {
		ti = 1
	}
	return list[ti-1].Name()
}

func chooseMode() string {
	fmt.Printf("\n%sحالت تونل:%s\n", ui.Bold, ui.Reset)
	fmt.Printf("  %s1%s) معکوس (Reverse) — خارج به ایران وصل می‌شود؛ ایران یک پورت باز دارد\n", ui.Cyan, ui.Reset)
	fmt.Printf("  %s2%s) مستقیم (Direct) — ایران بیرون زنگ می‌زند؛ هیچ پورت ورودی روی ایران لازم نیست\n", ui.Cyan, ui.Reset)
	if ask("حالت", "1") == "2" {
		return config.ModeDirect
	}
	return config.ModeReverse
}

func askForwards() []config.Forward {
	var fwds []config.Forward
	fmt.Printf("\n%sپورت‌های فوروارد%s (برای اتمام، نام را خالی بگذار)\n", ui.Bold, ui.Reset)
	for {
		name := ask("نام فوروارد", "")
		if name == "" {
			break
		}
		lp, _ := strconv.Atoi(ask("پورت ورودی (روی سرور ایران)", "443"))
		target := ask("مقصد روی سرور خارج (host:port)", "127.0.0.1:443")
		fwds = append(fwds, config.Forward{Name: name, Listen: lp, Target: target, Proto: "tcp"})
	}
	return fwds
}

func wizardRelay() {
	fmt.Printf("\n%s── نصب سرور ایران ──%s\n", ui.Bold, ui.Reset)
	mode := chooseMode()
	tr := chooseTransport()
	listenPort := ask("پورت تونل روی ایران", "7000")
	cfg := &config.Config{
		Role:      config.RoleRelay,
		Mode:      mode,
		Transport: tr,
		Listen:    ":" + listenPort,
		PSK:       config.GenPSK(),
		SNI:       ask("SNI (دامنهٔ تقلبی برای TLS)", "www.cloudflare.com"),
		Forwards:  askForwards(),
		Creator:   creator,
		GitHub:    github,
	}
	var advAddr string
	if mode == config.ModeReverse {
		ip := ask("آی‌پی عمومی این سرور ایران", "")
		advAddr = ip + ":" + listenPort
	} else {
		fip := ask("آی‌پی سرور خارج (ایران به آن وصل می‌شود)", "")
		fport := ask("پورت سرور خارج", listenPort)
		cfg.Peer = fip + ":" + fport
		advAddr = "0.0.0.0:" + fport
	}
	if tr == "crdv" {
		cfg.Broker = ask("آدرس بروکر (broker host:port)", cfg.Broker)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Println(ui.Red+"خطا:"+ui.Reset, err)
		return
	}
	saveAndMaybeService(cfg)
	setup := setupcode.FromRelay(cfg, advAddr)
	fmt.Printf("\n%s✓ سرور ایران آماده شد.%s کد ستاپ را روی سرور خارج اجرا کن:\n", ui.Green, ui.Reset)
	// Prefer a short code when a broker is available.
	if cfg.Broker != "" && ask("کد کوتاه بسازم؟ (نیاز به بروکر) (y/n)", "y") == "y" {
		code, err := setupcode.MintShort(cfg.Broker, setup)
		if err != nil {
			fmt.Println(ui.Yellow+"ساخت کد کوتاه ناموفق:"+ui.Reset, err)
		} else {
			fmt.Printf("  %s%s%s   (۳۰ دقیقه معتبر)\n", ui.Green, code, ui.Reset)
			fmt.Printf("  روی خارج: %sctunnel join %s -broker %s%s\n", ui.Cyan, code, cfg.Broker, ui.Reset)
			return
		}
	}
	code := setupcode.Encode(setup)
	fmt.Printf("  %sctunnel join %s%s\n", ui.Cyan, code, ui.Reset)
	fmt.Printf("\nیا نصب دستی: نقش exit، همان PSK و ترنسپورت %s.\n", tr)
}

func wizardExit() {
	fmt.Printf("\n%s── نصب سرور خارج ──%s\n", ui.Bold, ui.Reset)
	code := ask("کد ستاپ را بچسبان", "")
	if code == "" {
		fmt.Println(ui.Yellow + "کدی وارد نشد." + ui.Reset)
		return
	}
	var s setupcode.Setup
	var err error
	if setupcode.IsShort(code) {
		b := ask("آدرس بروکر (broker host:port)", "")
		s, err = setupcode.ResolveShort(b, code)
	} else {
		s, err = setupcode.Decode(code)
	}
	if err != nil {
		fmt.Println(ui.Red+"کد نامعتبر:"+ui.Reset, err)
		return
	}
	cfg := s.ExitConfig()
	cfg.Creator, cfg.GitHub = creator, github
	if err := cfg.Validate(); err != nil {
		fmt.Println(ui.Red+"خطا:"+ui.Reset, err)
		return
	}
	saveAndMaybeService(cfg)
	fmt.Printf("%s✓ سرور خارج به تونل وصل شد%s (mode=%s transport=%s)\n", ui.Green, ui.Reset, cfg.Mode, cfg.Transport)
}

func manageMenu() {
	for {
		fmt.Printf(`
  %sمدیریت%s
  %s1%s) وضعیت و پینگ زنده
  %s2%s) Link Test (تست همهٔ ترنسپورت‌ها)
  %s3%s) تغییر ترنسپورت
  %s4%s) ری‌استارت سرویس
  %s5%s) بازگشت
`, ui.Bold, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset, ui.Cyan, ui.Reset)
		switch ask("انتخاب", "") {
		case "1":
			cmdStatus(nil)
		case "2":
			manageLinkTest()
		case "3":
			name := chooseTransport()
			cmdSwitch([]string{name})
		case "4":
			serviceCmd("restart")
		case "5", "q":
			return
		}
	}
}

func manageLinkTest() {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		fmt.Println(ui.Red+"کانفیگ خوانده نشد:"+ui.Reset, err)
		return
	}
	if cfg.Role == config.RoleRelay {
		fmt.Println("روی ایران: سرور تست را اجرا می‌کنم، سپس روی خارج «ctunnel linktest -host <ip-ایران>» را بزن.")
		cmdLinktestServer(nil)
		return
	}
	cmdLinktest(nil)
}

// saveAndMaybeService writes the config and offers to install+start the systemd service.
func saveAndMaybeService(cfg *config.Config) {
	path := config.DefaultPath()
	if err := cfg.Save(path); err != nil {
		fmt.Println(ui.Red+"ذخیرهٔ کانفیگ ناموفق:"+ui.Reset, err)
		return
	}
	fmt.Printf("%s✓ کانفیگ ذخیره شد:%s %s\n", ui.Green, ui.Reset, path)
	if ask("سرویس systemd نصب و اجرا شود؟ (y/n)", "y") == "y" {
		if err := installService(path); err != nil {
			fmt.Println(ui.Yellow+"نصب سرویس ناموفق:"+ui.Reset, err)
			fmt.Printf("می‌توانی دستی اجرا کنی: %sctunnel run%s\n", ui.Cyan, ui.Reset)
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
AmbientCapabilities=CAP_NET_ADMIN
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
	fmt.Printf("%s✓ سرویس ctunnel فعال شد%s (systemctl status ctunnel)\n", ui.Green, ui.Reset)
	return nil
}

func serviceCmd(action string) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		fmt.Println(ui.Yellow + "systemctl در دسترس نیست." + ui.Reset)
		return
	}
	out, err := exec.Command("systemctl", action, "ctunnel").CombinedOutput()
	if err != nil {
		fmt.Printf("%s%s%s: %v\n%s\n", ui.Red, action, ui.Reset, err, out)
		return
	}
	fmt.Printf("%s✓ %s انجام شد%s\n", ui.Green, action, ui.Reset)
}
