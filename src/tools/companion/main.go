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
		reason := "invalid_configuration_or_runtime"
		if b, ok := err.(*companion.Blocked); ok {
			reason = b.Reason
		}
		_ = json.NewEncoder(os.Stdout).Encode(companion.Object{"state": "blocked", "reason": reason})
		return 2
	}
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
		if e = companion.Serve(os.Stdin, os.Stdout, h); e != nil {
			return 2
		}
		return 0
	case "adb-phone":
		w := &companion.PhoneWorker{NewTransport: companion.NewADB}
		if e := companion.Serve(os.Stdin, os.Stdout, w); e != nil {
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	results := []companion.Object{}
	exit := 0
	for _, a := range c.Accounts {
		if *account != "" && a.ID != *account {
			continue
		}
		r, e := (companion.Runner{}).Recover(ctx, a, state, *apply)
		if e != nil {
			return fail(e)
		}
		results = append(results, r)
		if r["state"] == "blocked" {
			exit = 2
		}
	}
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
