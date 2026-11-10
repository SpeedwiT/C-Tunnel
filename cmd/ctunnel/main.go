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
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		cmdRun(os.Args[2:])
	case "genpsk":
		fmt.Println(config.GenPSK())
	case "transports":
		for _, n := range transport.Names() {
			fmt.Println(n)
		}
	case "version", "-v", "--version":
		fmt.Printf("C Tunnel %s (%s)\n", version, github)
	case "menu", "-h", "--help":
		fmt.Print(ui.Banner(version, github, creator))
		usage()
	default:
		usage()
		os.Exit(2)
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

func usage() {
	fmt.Fprint(os.Stderr, `
C Tunnel — usage:
  ctunnel run [-config PATH]   run the node (role/transport from the config)
  ctunnel genpsk               print a fresh pre-shared key
  ctunnel transports           list the available transports
  ctunnel version              show the version
`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
