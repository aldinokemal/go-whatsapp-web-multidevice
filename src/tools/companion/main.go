// companion is an optional standalone controller. Building or running it does not
// initialize GoWA's REST server, schedulers, saved sessions, or ADB access.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/companion"
)

func main() { os.Exit(run()) }
func run() int {
	flags := flag.NewFlagSet("companion", flag.ContinueOnError)
	configPath := flags.String("config", "", "Private controller or HTTP backend JSON configuration")
	stateDir := flags.String("state-dir", "", "Private recovery state directory (0700)")
	account := flags.String("account", "", "Configured account identifier")
	apply := flags.Bool("apply", false, "Enable bounded recovery and phone input (default: inspect only)")
	mode := flags.String("mode", "recover", "recover, http-backend, or adb-phone")
	if flags.Parse(os.Args[1:]) != nil {
		return 2
	}
	fail := func(err error) int {
		_ = json.NewEncoder(os.Stdout).Encode(companion.Object{"state": "blocked", "reason": errorReason(err)})
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch *mode {
	case "http-backend":
		var c companion.HTTPConfig
		b, e := os.ReadFile(*configPath)
		if e != nil {
			return fail(e)
		}
		if e = json.Unmarshal(b, &c); e != nil {
			return fail(e)
		}
		h, e := companion.NewHTTPBackend(c)
		if e != nil {
			return fail(e)
		}
		if e = companion.ServeContext(ctx, os.Stdin, os.Stdout, h); e != nil {
			return 2
		}
		return 0
	case "adb-phone":
		w := &companion.PhoneWorker{NewTransport: companion.NewADB}
		if e := companion.ServeContext(ctx, os.Stdin, os.Stdout, w); e != nil {
			return 2
		}
		return 0
	case "recover":
	default:
		return fail(&companion.Blocked{Reason: "invalid_request"})
	}
	c, e := companion.LoadConfig(*configPath)
	if e != nil {
		return fail(e)
	}
	if *stateDir == "" {
		return fail(&companion.Blocked{Reason: "invalid_request"})
	}
	state, e := companion.OpenState(*stateDir)
	if e != nil {
		return fail(e)
	}
	defer state.Close()
	results, exit := runAccounts(ctx, companion.Runner{}, c.Accounts, state, *account, *apply)
	if len(results) == 0 {
		return fail(&companion.Blocked{Reason: "invalid_request"})
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encoder.Encode(companion.Object{"results": results}) != nil {
		return 2
	}
	return exit
}

func errorReason(err error) string {
	if b, ok := err.(*companion.Blocked); ok {
		return b.Reason
	}
	return "invalid_configuration_or_runtime"
}

func runAccounts(ctx context.Context, runner companion.Runner, accounts []companion.Account, state *companion.State, selected string, apply bool) ([]companion.Object, int) {
	results := []companion.Object{}
	exit := 0
	for _, a := range accounts {
		if selected != "" && a.ID != selected {
			continue
		}
		r, e := runner.Recover(ctx, a, state, apply)
		if e != nil {
			r = companion.Object{"account": a.ID, "state": "blocked", "reason": errorReason(e)}
		}
		results = append(results, r)
		if r["state"] == "blocked" {
			exit = 2
		}
		if ctx.Err() != nil {
			break
		}
	}
	return results, exit
}
