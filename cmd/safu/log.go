package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/StackSatoshis/safu/internal/config"
	safulog "github.com/StackSatoshis/safu/internal/log"
	"github.com/StackSatoshis/safu/internal/tui"
)

// logCmd implements the non-interactive `safu log` views. The fuzzy interactive
// browser arrives with the TUI slice.
func logCmd(args []string) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit raw JSON lines")
	grep := fs.String("grep", "", "only lines containing this substring")
	since := fs.String("since", "", "only events on/after this time (RFC3339 or 2006-01-02)")
	clear := fs.Bool("clear", false, "wipe the activity log")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	logger := safulog.New(config.Expand(cfg.Log.Dir), cfg.Log.ActivityRetentionDays)

	if *clear {
		if err := logger.Clear(); err != nil {
			return err
		}
		fmt.Println("activity log cleared")
		return nil
	}

	var filter safulog.Filter
	filter.Grep = *grep
	if *since != "" {
		t, err := parseSince(*since)
		if err != nil {
			return err
		}
		filter.Since = t
	}

	events, err := logger.Read(filter)
	if err != nil {
		return err
	}

	// Interactive browser when on a TTY with no output-shaping flags.
	if !*asJSON && *grep == "" && *since == "" && isInteractive() {
		if len(events) == 0 {
			fmt.Println("no activity logged yet")
			return nil
		}
		return tui.RunLogBrowser(events, time.Now())
	}

	if len(events) == 0 {
		fmt.Println("no matching activity")
		return nil
	}

	enc := json.NewEncoder(os.Stdout)
	for _, e := range events {
		if *asJSON {
			_ = enc.Encode(e)
			continue
		}
		line := fmt.Sprintf("%s  %-12s  %s", e.Time.Local().Format("2006-01-02 15:04:05"), e.Kind, e.Command)
		if e.Risk != "" {
			line += "  [" + e.Risk + "]"
		}
		fmt.Println(line)
	}
	return nil
}

func parseSince(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q (want RFC3339 or 2006-01-02)", s)
}
