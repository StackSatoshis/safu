package main

import (
	"flag"
	"fmt"

	"github.com/StackSatoshis/safu/internal/config"
	"github.com/StackSatoshis/safu/internal/guard"
	safulog "github.com/StackSatoshis/safu/internal/log"
)

// undoCmd implements `safu undo [--list]`: restore the most recent soft-deleted
// operation, or list the trash history.
func undoCmd(args []string) error {
	fs := flag.NewFlagSet("undo", flag.ContinueOnError)
	list := fs.Bool("list", false, "list trashed operations instead of restoring")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	trashDir := config.Expand(cfg.Guard.TrashDir)

	if *list {
		ops, err := guard.ListOps(trashDir)
		if err != nil {
			return err
		}
		if len(ops) == 0 {
			fmt.Println("trash is empty")
			return nil
		}
		for _, m := range ops {
			fmt.Printf("%s  %s  (%d item(s))  %s\n",
				m.Time.Local().Format("2006-01-02 15:04:05"), m.OpID, len(m.Entries), m.Command)
		}
		return nil
	}

	m, err := guard.Undo(trashDir)
	if err != nil {
		return err
	}
	fmt.Printf("restored %d item(s) from %s\n", len(m.Entries), m.OpID)
	for _, e := range m.Entries {
		fmt.Printf("  %s\n", e.Original)
	}

	if cfg.Log.Enabled {
		logger := safulog.New(config.Expand(cfg.Log.Dir), cfg.Log.ActivityRetentionDays)
		_ = logger.Append(safulog.Event{
			Kind: safulog.KindUndo, Command: m.Command,
			Detail: map[string]any{"op_id": m.OpID, "count": len(m.Entries)},
		})
	}
	return nil
}
