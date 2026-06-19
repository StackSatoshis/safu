package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/StackSatoshis/safu/internal/config"
	"github.com/StackSatoshis/safu/internal/nav"
)

// navCmd implements `safu z`:
//
//	safu z --add <path>        record a visit (called by the shell hook)
//	safu z --resolve -- <q>    print the best-matching directory (for `cd`)
//	safu z --list              show the ranked database
//	safu z --clear             wipe the database
//	safu z <query>             same as --resolve (scriptable)
func navCmd(args []string) error {
	fs := flag.NewFlagSet("z", flag.ContinueOnError)
	add := fs.String("add", "", "record a visit to this directory")
	// --resolve is the default action; the flag is accepted for explicitness.
	_ = fs.Bool("resolve", false, "print the best-matching directory and exit")
	list := fs.Bool("list", false, "list the ranked directory database")
	clear := fs.Bool("clear", false, "wipe the navigation database")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	dataDir := config.Expand(cfg.Navigation.DataDir)

	// --add: tracking path. Silently no-op when nav is disabled, the path is
	// excluded, or outbound/privacy switches are set (§7.3).
	if *add != "" {
		if !cfg.Navigation.Enabled || cfg.Network.Offline {
			return nil
		}
		if nav.IsExcluded(*add, cfg.Navigation.Exclude) {
			return nil
		}
		db, err := nav.Open(dataDir, cfg.Navigation.MaxEntries)
		if err != nil {
			return err
		}
		return db.Add(*add)
	}

	db, err := nav.Open(dataDir, cfg.Navigation.MaxEntries)
	if err != nil {
		return err
	}

	switch {
	case *clear:
		if err := db.Clear(); err != nil {
			return err
		}
		fmt.Println("navigation database cleared")
		return nil

	case *list:
		entries := db.List()
		if len(entries) == 0 {
			fmt.Println("navigation database is empty")
			return nil
		}
		for _, e := range entries {
			fmt.Printf("%8.2f  %s\n", db.Score(e), e.Path)
		}
		return nil

	default: // --resolve or bare query
		query := strings.Join(fs.Args(), " ")
		target, ok := db.Query(query)
		if !ok {
			// Non-zero exit so the shell `z` function does not cd.
			fmt.Fprintf(os.Stderr, "safu: no directory matching %q\n", query)
			return errSilent
		}
		fmt.Println(target)
		return nil
	}
}
