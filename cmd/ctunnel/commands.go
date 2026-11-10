package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/SpeedwiT/c-tunnel/internal/broker"
	"github.com/SpeedwiT/c-tunnel/internal/config"
	"github.com/SpeedwiT/c-tunnel/internal/linktest"
	"github.com/SpeedwiT/c-tunnel/internal/setupcode"
	"github.com/SpeedwiT/c-tunnel/internal/transport"
	"github.com/SpeedwiT/c-tunnel/internal/ui"
)

// cmdBroker runs the rendezvous broker: both servers dial out to it, it pairs and pipes them.
func cmdBroker(args []string) {
	fs := flag.NewFlagSet("broker", flag.ExitOnError)
	listen := fs.String("listen", ":9000", "address to listen on")
	_ = fs.Parse(args)
	fmt.Printf("%sC Tunnel broker روی %s%s — هر دو سرور به این آدرس وصل می‌شوند.\n", ui.Green, *listen, ui.Reset)
	if err := broker.Serve(*listen); err != nil {
		fatal(err)
	}
}

// linkTestBasePort is where the Link Test probe listeners start; each transport takes the next port.
const linkTestBasePort = 8900

// cmdSetupCode prints the setup code the foreign server consumes with `ctunnel join`.
func cmdSetupCode(args []string) {
	fs := flag.NewFlagSet("setup-code", flag.ExitOnError)
	path := fs.String("config", config.DefaultPath(), "config file")
	addr := fs.String("addr", "", "address the other server relates to (host:port). "+
		"reverse: this server's public ip:port; direct: the foreign server's ip:port")
	short := fs.Bool("short", false, "mint a short C-Tunnel-XXXX code via the broker")
	brokerAddr := fs.String("broker", "", "broker address for -short (default: config broker)")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		fatal(err)
	}
	a := *addr
	if a == "" {
		a = defaultPeerAddr(cfg)
	}
	if a == "" || strings.HasPrefix(a, ":") {
		fatal(fmt.Errorf("could not determine the address; pass -addr host:port"))
	}
	setup := setupcode.FromRelay(cfg, a)
	if *short {
		b := *brokerAddr
		if b == "" {
			b = cfg.Broker
		}
		if b == "" {
			fatal(fmt.Errorf("short code needs a broker; pass -broker host:port"))
		}
		code, err := setupcode.MintShort(b, setup)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("\n%sکد کوتاه ستاپ (۳۰ دقیقه معتبر):%s\n", ui.Bold, ui.Reset)
		fmt.Printf("  %s%s%s\n", ui.Green, code, ui.Reset)
		fmt.Printf("  روی سرور خارج: %sctunnel join %s -broker %s%s\n\n", ui.Cyan, code, b, ui.Reset)
		return
	}
	code := setupcode.Encode(setup)
	fmt.Println()
	fmt.Printf("%sکد ستاپ سرور خارج (روی سرور خارج اجرا کن):%s\n", ui.Bold, ui.Reset)
	fmt.Printf("  %sctunnel join %s%s\n\n", ui.Cyan, code, ui.Reset)
}

// defaultPeerAddr derives the address to advertise from the config when -addr is not given.
func defaultPeerAddr(cfg *config.Config) string {
	port := portOf(cfg.Listen)
	if port == "" {
		port = portOf(cfg.Peer)
	}
	host := hostOf(cfg.Peer)
	if host == "" {
		return ""
	}
	return host + ":" + port
}

// cmdJoin consumes a setup code on the foreign server and writes its config.
func cmdJoin(args []string) {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	path := fs.String("config", config.DefaultPath(), "config file")
	brokerAddr := fs.String("broker", "", "broker address (required for a short C-Tunnel-XXXX code)")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) != 1 {
		fatal(fmt.Errorf("usage: ctunnel join <setup-code> [-broker host:port]"))
	}
	var s setupcode.Setup
	var err error
	if setupcode.IsShort(rest[0]) {
		if *brokerAddr == "" {
			fatal(fmt.Errorf("a short code needs the broker: ctunnel join %s -broker host:port", rest[0]))
		}
		s, err = setupcode.ResolveShort(*brokerAddr, rest[0])
	} else {
		s, err = setupcode.Decode(rest[0])
	}
	if err != nil {
		fatal(err)
	}
	cfg := s.ExitConfig()
	cfg.Creator, cfg.GitHub = creator, github
	if err := cfg.Validate(); err != nil {
		fatal(err)
	}
	if err := cfg.Save(*path); err != nil {
		fatal(err)
	}
	fmt.Printf("%s✓ سرور خارج پیکربندی شد%s  (mode=%s transport=%s)\n", ui.Green, ui.Reset, cfg.Mode, cfg.Transport)
	fmt.Printf("  اجرا: %sctunnel run%s   یا سرویس: %sctunnel install-service%s\n", ui.Cyan, ui.Reset, ui.Cyan, ui.Reset)
}

// cmdLinktestServer runs the probe server on the relay (Iran) so the foreign server can test links.
func cmdLinktestServer(args []string) {
	fs := flag.NewFlagSet("linktest-server", flag.ExitOnError)
	path := fs.String("config", config.DefaultPath(), "config file")
	base := fs.Int("base", linkTestBasePort, "base port for probe listeners")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		fatal(err)
	}
	names := transport.Names()
	stop, err := linktest.Server(cfg, names, *base)
	if err != nil {
		fatal(err)
	}
	defer stop()
	fmt.Printf("%sLink Test server آماده است%s روی پورت‌های %d..%d\n", ui.Green, ui.Reset, *base, *base+len(names)-1)
	fmt.Println("روی سرور خارج اجرا کن: ctunnel linktest -host <ip-ایران>")
	fmt.Println("برای توقف Ctrl+C بزن.")
	select {} // block until killed
}

// cmdLinktest probes every transport from the foreign server and prints a ranked table.
func cmdLinktest(args []string) {
	fs := flag.NewFlagSet("linktest", flag.ExitOnError)
	path := fs.String("config", config.DefaultPath(), "config file")
	host := fs.String("host", "", "relay (Iran) host to probe; default: peer from config")
	base := fs.Int("base", linkTestBasePort, "base port the relay's probe server uses")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		fatal(err)
	}
	h := *host
	if h == "" {
		h = hostOf(cfg.Peer)
	}
	if h == "" {
		fatal(fmt.Errorf("no host to probe; pass -host <ip>"))
	}
	names := transport.Names()
	fmt.Printf("در حال تست %d ترنسپورت به %s ...\n", len(names), h)
	results := linktest.ProbeAll(cfg, names, h, *base, 6*time.Second)
	printResults(results)
}

// printResults renders Link Test results as a colored table, best first.
func printResults(results []linktest.Result) {
	fmt.Printf("\n  %-9s %-8s %-10s %-10s %s\n", "transport", "cat", "connect", "ping", "status")
	fmt.Println("  " + strings.Repeat("─", 52))
	for _, r := range results {
		status := ui.Green + "OK" + ui.Reset
		ping := fmt.Sprintf("%dms", r.Ping.Milliseconds())
		if !r.OK {
			status = ui.Red + "fail" + ui.Reset + ui.Dim + " (" + short(r.Err) + ")" + ui.Reset
			ping = "-"
		}
		fmt.Printf("  %s%-9s%s %-8s %-10s %-10s %s\n",
			ui.Cyan, r.Transport, ui.Reset, r.Category,
			fmt.Sprintf("%dms", r.Connect.Milliseconds()), ping, status)
	}
	best := ""
	for _, r := range results {
		if r.OK {
			best = r.Transport
			break
		}
	}
	if best != "" {
		fmt.Printf("\n  %sبهترین ترنسپورت: %s%s — برای استفاده: ctunnel switch %s\n\n", ui.Bold, best, ui.Reset, best)
	} else {
		fmt.Printf("\n  %sهیچ ترنسپورتی وصل نشد — آیا linktest-server روی ایران اجراست؟%s\n\n", ui.Yellow, ui.Reset)
	}
}

// cmdSwitch changes the transport in the saved config without reinstalling.
func cmdSwitch(args []string) {
	fs := flag.NewFlagSet("switch", flag.ExitOnError)
	path := fs.String("config", config.DefaultPath(), "config file")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) != 1 {
		fatal(fmt.Errorf("usage: ctunnel switch <transport>"))
	}
	name := rest[0]
	if _, err := transport.Get(name); err != nil {
		fatal(err)
	}
	cfg, err := config.Load(*path)
	if err != nil {
		fatal(err)
	}
	old := cfg.Transport
	cfg.Transport = name
	if err := cfg.Save(*path); err != nil {
		fatal(err)
	}
	fmt.Printf("%s✓ ترنسپورت از %s به %s تغییر کرد%s\n", ui.Green, old, name, ui.Reset)
	fmt.Printf("  روی هر دو سرور %sctunnel restart%s (یا systemctl restart ctunnel) را بزن.\n", ui.Cyan, ui.Reset)
}

// cmdStatus prints the config summary and, where possible, a live ping.
func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	path := fs.String("config", config.DefaultPath(), "config file")
	_ = fs.Parse(args)
	cfg, err := config.Load(*path)
	if err != nil {
		fatal(err)
	}
	roleFa := map[string]string{config.RoleRelay: "ایران (relay)", config.RoleExit: "خارج (exit)"}
	fmt.Println()
	fmt.Printf("  %sC Tunnel — وضعیت%s\n", ui.Bold, ui.Reset)
	fmt.Printf("  نقش:        %s\n", roleFa[cfg.Role])
	fmt.Printf("  حالت:       %s\n", cfg.Mode)
	fmt.Printf("  ترنسپورت:   %s\n", cfg.Transport)
	if cfg.Listen != "" {
		fmt.Printf("  listen:     %s\n", cfg.Listen)
	}
	if cfg.Peer != "" {
		fmt.Printf("  peer:       %s\n", cfg.Peer)
	}
	fmt.Printf("  پورت‌ها:     %d فوروارد\n", len(cfg.Forwards))
	for _, f := range cfg.Forwards {
		fmt.Printf("    • %s  :%d → %s (%s)\n", f.Name, f.Listen, f.Target, protoOr(f.Proto))
	}
	// Live ping only makes sense from the side that dials out.
	if cfg.Role == config.RoleExit && cfg.Peer != "" {
		fmt.Printf("  در حال پینگ %s ...\n", cfg.Peer)
		r := linktest.ProbeAddr(cfg, cfg.Transport, cfg.Peer, 6*time.Second)
		if r.OK {
			fmt.Printf("  لینک:       %sمتصل%s  (ping %dms)\n", ui.Green, ui.Reset, r.Ping.Milliseconds())
		} else {
			fmt.Printf("  لینک:       %sقطع%s  (%s)\n", ui.Red, ui.Reset, short(r.Err))
		}
	}
	fmt.Println()
}

func short(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 32 {
		return s[:32] + "…"
	}
	return s
}

func protoOr(p string) string {
	if p == "" {
		return "tcp"
	}
	return p
}

func portOf(addr string) string {
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		return addr[i+1:]
	}
	return ""
}

func hostOf(addr string) string {
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		return addr[:i]
	}
	return addr
}

var _ = os.Stdout
