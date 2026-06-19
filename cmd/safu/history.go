package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/StackSatoshis/safu/internal/config"
	safulog "github.com/StackSatoshis/safu/internal/log"
	"github.com/StackSatoshis/safu/internal/tui"
)

// historyCmd implements `safu history`:
//
//	--add --exit N --dir D -- <cmd>   record a command (called by the shell hook)
//	(no flags, TTY)                   interactive fuzzy browser; prints the pick
//	--search Q | --list | --json      non-interactive views
//	--clear                           wipe history
func historyCmd(args []string) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	add := fs.Bool("add", false, "record a command (used by the shell hook)")
	exit := fs.Int("exit", 0, "exit code of the recorded command")
	dir := fs.String("dir", "", "working directory of the recorded command")
	search := fs.String("search", "", "filter to commands containing this text")
	list := fs.Bool("list", false, "print history (non-interactive)")
	asJSON := fs.Bool("json", false, "emit raw JSON entries")
	clear := fs.Bool("clear", false, "wipe the history log")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	h := safulog.NewHistory(config.Expand(cfg.Log.Dir), cfg.Log.HistoryRetentionDays)

	if *add {
		cmd := strings.TrimSpace(strings.Join(fs.Args(), " "))
		// Defense in depth: honor the toggle, offline switch, and exclude list
		// even though the hook also gates on them (§8.3).
		if cmd == "" || !cfg.Log.History || cfg.Network.Offline {
			return nil
		}
		if safulog.HistoryExcluded(cmd, cfg.Log.HistoryExclude) {
			return nil
		}
		return h.Append(safulog.HistoryEntry{Command: cmd, Dir: *dir, Exit: *exit})
	}

	if *clear {
		if err := h.Clear(); err != nil {
			return err
		}
		fmt.Println("shell history cleared")
		return nil
	}

	_ = h.Trim()
	entries, err := h.Read(*search)
	if err != nil {
		return err
	}

	// Interactive browser: a TTY, and no output-shaping flags.
	if !*list && !*asJSON && *search == "" && isInteractive() {
		if len(entries) == 0 {
			fmt.Fprintln(os.Stderr, "no shell history recorded yet")
			return nil
		}
		sel, err := tui.RunHistoryBrowser(entries, time.Now())
		if err != nil {
			return err
		}
		if sel != "" {
			fmt.Println(sel) // stdout = the selection, for the Ctrl-R widget
		}
		return nil
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		for _, e := range entries {
			_ = enc.Encode(e)
		}
		return nil
	}

	deduped := safulog.DedupLatest(entries)
	if len(deduped) == 0 {
		fmt.Fprintln(os.Stderr, "no matching history")
		return nil
	}
	now := time.Now()
	// newest-first, matching the interactive browser.
	for i := len(deduped) - 1; i >= 0; i-- {
		e := deduped[i]
		fmt.Printf("%-12s  %s\n", tui.HumanAge(e.Time, now), e.Command)
	}
	return nil
}
