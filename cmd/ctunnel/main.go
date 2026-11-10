// Command ctunnel is the C Tunnel node: a fast, anti-DPI, anti-filter tunnel between an Iran relay
// and a foreign exit, with multiple transports and multi-port forwarding.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/SpeedwiT/c-tunnel/internal/config"
	"github.com/SpeedwiT/c-tunnel/internal/transport"
	"github.com/SpeedwiT/c-tunnel/internal/tunnel"
	"github.com/SpeedwiT/c-tunnel/internal/ui"
)

// set by the build (-ldflags "-X main.version=...").
var (
	version = "dev"
	github  = "@SpeedwiT"
	creator = "SpeedwiT"
)

func main() {
	if len(os.Args) < 2 {
		runMenu() // interactive terminal menu
		return
	}
	switch os.Args[1] {
	case "run":
		cmdRun(os.Args[2:])
	case "genpsk":
		fmt.Println(config.GenPSK())
	case "transports":
		printTransports()
	case "setup-code":
		cmdSetupCode(os.Args[2:])
	case "join":
		cmdJoin(os.Args[2:])
	case "broker":
		cmdBroker(os.Args[2:])
	case "linktest-server":
		cmdLinktestServer(os.Args[2:])
	case "linktest", "test":
		cmdLinktest(os.Args[2:])
	case "switch":
		cmdSwitch(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "install-service":
		installServiceFromArgs(os.Args[2:])
	case "restart", "stop", "start":
		serviceCmd(os.Args[1])
	case "menu":
		runMenu()
	case "version", "-v", "--version":
		fmt.Printf("C Tunnel %s (%s)\n", version, github)
	case "-h", "--help":
		fmt.Print(ui.Banner(version, github, creator))
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func installServiceFromArgs(args []string) {
	path := config.DefaultPath()
	if len(args) == 2 && args[0] == "-config" {
		path = args[1]
	}
	if err := installService(path); err != nil {
		fatal(err)
	}
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	path := fs.String("config", config.DefaultPath(), "config file")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cfg.Role {
	case config.RoleRelay:
		err = tunnel.RunRelay(ctx, cfg)
	case config.RoleExit:
		err = tunnel.RunExit(ctx, cfg)
	default:
		fatal(fmt.Errorf("unknown role %q", cfg.Role))
	}
	if err != nil {
		fatal(err)
	}
}

func printTransports() {
	grouped := transport.Grouped()
	for _, cat := range transport.CategoryOrder {
		list := grouped[cat]
		if len(list) == 0 {
			continue
		}
		fmt.Printf("\n%s%s%s\n", ui.Bold, transport.CategoryTitle[cat], ui.Reset)
		for _, t := range list {
			fmt.Printf("  %s%-8s%s %s\n", ui.Cyan, t.Name(), ui.Reset, t.Description())
		}
	}
	fmt.Println()
}

func usage() {
	fmt.Fprint(os.Stderr, `
C Tunnel — usage:
  ctunnel                      interactive install/manage menu
  ctunnel run [-config PATH]   run the node (role/transport from the config)
  ctunnel setup-code [-addr H] print the code the foreign server joins with
  ctunnel join <code>          configure the foreign server from a setup code
  ctunnel status               show role, mode, forwards and a live ping
  ctunnel linktest [-host IP]  test every transport and rank by ping (foreign side)
  ctunnel linktest-server      serve the Link Test probes (Iran side)
  ctunnel switch <transport>   change transport without reinstalling
  ctunnel install-service      install + start the systemd service
  ctunnel restart|stop|start   control the systemd service
  ctunnel genpsk               print a fresh pre-shared key
  ctunnel transports           list the available transports
  ctunnel version              show the version
`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
