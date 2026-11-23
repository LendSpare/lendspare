package app

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// The command line: `lendspare connect`, `lendspare connections`, `lendspare disconnect <id>`. They work
// on the same files as a running LendSpare (which notices the change), and `connect` asks for the
// same local confirmation as the settings page does.

func runSubcommand(name string, args []string) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var cfg config
	fs.Var(devFlag{&cfg.devOrigins}, "dev-origin", "development only: also allow this provider origin (repeatable)")
	provider := fs.String("provider", "", "connect: the provider's host, e.g. one from the list `lendspare connect` shows")
	code := fs.String("code", "", "connect: the code shown on the website (XXXX-XXXX)")
	yes := fs.Bool("yes", false, "connect: do not ask - for scripts that already showed the person the details")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	switch name {
	case "version":
		fmt.Println("LendSpare", daemonVersion)
		return 0
	case "connect", "connections", "disconnect":
	default:
		fmt.Fprintf(os.Stderr, "lendspare: unknown command %q (connect, connections, disconnect, or none to run)\n", name)
		return 2
	}
	a, err := openApp(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lendspare:", err)
		return 1
	}
	var out error
	switch name {
	case "connect":
		out = cliConnect(a, *provider, *code, *yes, os.Stdin, os.Stdout)
	case "connections":
		cliConnections(a, os.Stdout)
	case "disconnect":
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: lendspare disconnect <id>")
			return 2
		}
		out = cliDisconnect(a, fs.Arg(0), os.Stdout)
	}
	if out != nil {
		fmt.Fprintln(os.Stderr, "lendspare:", out)
		return 1
	}
	return 0
}

func cliConnections(a *app, w io.Writer) {
	list := a.mgr.list()
	if len(list) == 0 {
		fmt.Fprintln(w, textReady+". No connections. "+textReadyHint)
		return
	}
	for _, c := range list {
		v := a.mgr.view(c)
		state := fmt.Sprint(v["state"])
		if c.Paused && state == stateConnected {
			state = "paused"
		}
		fmt.Fprintf(w, "%s  %s  %s  account=%s  scopes=%s  %s  compute=%v\n", c.ID, hostOf(c.Origin), serviceName(c.Service),
			firstNonEmpty(c.Account, "-"), strings.Join(c.Scopes, ","), state, c.Compute)
	}
}

func cliDisconnect(a *app, id string, w io.Writer) error {
	c, ok := a.mgr.get(id)
	if !ok {
		return fmt.Errorf("no connection with id %s (see `lendspare connections`)", id)
	}
	notice, err := a.mgr.disconnect(id)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Disconnected from %s.\n", hostOf(c.Origin))
	if notice != "" {
		fmt.Fprintln(w, notice)
	}
	return nil
}

// cliConnect: choose a provider, then either confirm a typed code or start from here.
func cliConnect(a *app, provider, code string, yes bool, in io.Reader, w io.Writer) error {
	r := bufio.NewReader(in)
	if provider == "" {
		list := a.mgr.policy.providers()
		fmt.Fprintln(w, "Service providers:")
		for i, o := range list {
			fmt.Fprintf(w, "  %d. %s\n", i+1, hostOf(o))
		}
		fmt.Fprint(w, "Choose one: ")
		line, _ := r.ReadString('\n')
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "%d", &n); err != nil || n < 1 || n > len(list) {
			return fmt.Errorf("choose a number from 1 to %d", len(list))
		}
		provider = hostOf(list[n-1])
		if code == "" {
			fmt.Fprint(w, "Code from the website (empty to connect from here): ")
			line, _ = r.ReadString('\n')
			code = strings.TrimSpace(line)
		}
	}
	var v map[string]any
	var err error
	if code != "" {
		v, err = a.pair.fromCode(provider, code)
	} else {
		v, err = a.pair.startDevice(provider)
	}
	if err != nil {
		return err
	}
	id := fmt.Sprint(v["id"])
	fmt.Fprintf(w, "\n%v\n%v\n", v["title"], v["summary"])
	for _, d := range v["details"].([]string) {
		fmt.Fprintf(w, "\n%s\n", d)
	}
	if c := fmt.Sprint(v["code"]); c != "" {
		fmt.Fprintf(w, "\nCode on the website: %s\n", c)
	}
	if !yes {
		fmt.Fprint(w, "\nAllow connection? [y/N] ")
		line, _ := r.ReadString('\n')
		if ans := strings.ToLower(strings.TrimSpace(line)); ans != "y" && ans != "yes" {
			_ = a.pair.cancel(id)
			fmt.Fprintln(w, "Cancelled. Nothing was connected.")
			return nil
		}
	}
	res, err := a.pair.allow(id)
	if err != nil {
		return err
	}
	if res["waiting"] == true {
		return waitForDevice(a, id, w)
	}
	fmt.Fprintf(w, "Connected to %s. Computing is off until you switch it on in LendSpare; no folder is shared until you grant it.\n", provider)
	return nil
}

func waitForDevice(a *app, id string, w io.Writer) error {
	shown := false
	for {
		e, ok := a.pair.take(id)
		if !ok {
			return fmt.Errorf("the request was cancelled")
		}
		a.pair.mu.Lock()
		v := a.pair.viewOf(e)
		a.pair.mu.Unlock()
		if msg := fmt.Sprint(v["error"]); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		if !shown && v["userCode"] != "" {
			fmt.Fprintf(w, "\nTo finish, open:\n    %v\nand press Allow if the code is  %v\n", v["verifyUrl"], v["userCode"])
			shown = true
		}
		if v["done"] == true {
			fmt.Fprintf(w, "Connected to %v. Computing is off until you switch it on in LendSpare; no folder is shared until you grant it.\n", v["host"])
			return nil
		}
		time.Sleep(time.Second)
	}
}
