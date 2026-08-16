package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

var (
	pruneDevicesApply bool
	pruneDevicesYes   bool
)

// pruneDevicesCmd removes orphan whatsmeow companion session rows -- rows in the
// whatsmeow_device store that no device slot references any more. Like every subcommand
// it runs the standard cobra.OnInitialize bootstrap (initApp: schema init/migration and
// InitWaCLI), so it reuses the exact same DB containers and chat-storage registry the
// service builds -- but it never starts the REST server and never dials WhatsApp on its
// own. Dry-run by default; deletes only with --apply.
var pruneDevicesCmd = &cobra.Command{
	Use:   "prune-devices",
	Short: "Remove orphan whatsmeow companion sessions not referenced by any device slot",
	Long: `Remove orphan whatsmeow companion session rows from the whatsmeow_device store.

Over time the whatsmeow session store accumulates companion rows for sessions that no
named device slot references any more (e.g. after re-pairings). This command lists those
orphan rows and, with --apply, deletes them.

A row is a prune CANDIDATE when its full AD JID (dev.ID.String()) is NOT the persisted
AD JID (the ADJID/ad_jid field) of any slot in the devices table. Only the PERSISTED AD
JID is consulted: this
is a separate process and cannot see a running service's in-memory registry.

SAFETY GATES
  1. Un-backfilled slots: if any slot still has an empty AD JID (legacy / not yet
     backfilled), it claims only a bare number. Any candidate row carrying such a number
     is SKIPPED, because we cannot prove it is an orphan rather than that slot's live
     companion. Backfill the AD JIDs (reconnect the slots) before pruning those numbers.
  2. No per-row age guard is available: whatsmeow's store.Device exposes no creation
     timestamp, so a freshly-created (mid-pairing) row cannot be detected here.

*** STOP THE GOWA SERVICE BEFORE RUNNING --apply. ***
This command opens the same SQLite session DB the running service uses. Deleting a row
while the service is live risks concurrent-access corruption and can destroy a session
that is being paired at that moment. Dry-run (the default) is read-only and safe to run
anytime, but --apply must be run only while the service is stopped.

By default this command is a DRY-RUN and deletes nothing. Pass --apply to delete, and
--yes to skip the interactive confirmation.`,
	Run: pruneDevices,
}

func init() {
	pruneDevicesCmd.Flags().BoolVar(
		&pruneDevicesApply,
		"apply",
		false,
		"actually delete orphan companion rows (default: dry-run, deletes nothing)",
	)
	pruneDevicesCmd.Flags().BoolVar(
		&pruneDevicesYes,
		"yes",
		false,
		"skip the interactive confirmation prompt (only meaningful together with --apply)",
	)
	rootCmd.AddCommand(pruneDevicesCmd)
}

// pruneCandidate is one whatsmeow store row considered for deletion.
type pruneCandidate struct {
	container *sqlstore.Container
	label     string
	dev       *store.Device
	adJID     string
	nonAD     string
}

func pruneDevices(_ *cobra.Command, _ []string) {
	ctx := context.Background()

	// Reuse the containers + registry constructed by initApp (cobra.OnInitialize). We
	// deliberately do NOT build a client or connect: this is a maintenance path.
	primary, keys := whatsapp.GetStoreContainers()
	if primary == nil {
		logrus.Fatalf("[PRUNE] whatsmeow store container not initialized")
	}
	if chatStorageRepo == nil {
		logrus.Fatalf("[PRUNE] chat storage repository not initialized")
	}

	records, err := chatStorageRepo.ListDeviceRecords()
	if err != nil {
		logrus.Fatalf("[PRUNE] failed to list device records: %v", err)
	}

	// Referenced set = every non-empty persisted AD JID. Un-backfilled slots (empty
	// AD JID) instead contribute their bare number to the protected-numbers set
	// (safety gate 1).
	referenced := make(map[string]bool)
	unbackfilledNumbers := make(map[string]bool)
	for _, rec := range records {
		if rec == nil {
			continue
		}
		if adJID := strings.TrimSpace(rec.ADJID); adJID != "" {
			referenced[adJID] = true
		} else if num := strings.TrimSpace(rec.JID); num != "" {
			unbackfilledNumbers[num] = true
		}
	}

	// Enumerate both containers (keys only when it is a distinct container).
	type containerRef struct {
		label     string
		container *sqlstore.Container
	}
	containers := []containerRef{{"primary", primary}}
	if keys != nil && keys != primary {
		containers = append(containers, containerRef{"keys", keys})
	}

	var candidates []pruneCandidate
	skipped := 0
	for _, cr := range containers {
		devices, err := cr.container.GetAllDevices(ctx)
		if err != nil {
			logrus.Fatalf("[PRUNE] failed to enumerate %s store devices: %v", cr.label, err)
		}
		for _, dev := range devices {
			if dev == nil || dev.ID == nil {
				continue
			}
			adJID := dev.ID.String()
			if referenced[adJID] {
				continue // referenced by a backfilled slot -> live, keep it
			}
			nonAD := dev.ID.ToNonAD().String()

			// Safety gate 1: protect rows whose number still has an un-backfilled slot.
			if unbackfilledNumbers[nonAD] {
				logrus.Warnf("[PRUNE] SKIP %s (%s store): number %s has an un-backfilled slot; cannot prove this row is an orphan vs. that slot's live companion -- backfill its AD JID first", adJID, cr.label, nonAD)
				skipped++
				continue
			}

			// No per-row age guard: store.Device carries no creation timestamp, so we
			// cannot skip a freshly-created (mid-pairing) row here. This is why the
			// service MUST be stopped before --apply (documented in Long help).
			candidates = append(candidates, pruneCandidate{
				container: cr.container,
				label:     cr.label,
				dev:       dev,
				adJID:     adJID,
				nonAD:     nonAD,
			})
		}
	}

	fmt.Println()
	fmt.Printf("Slots referenced (kept) by AD JID: %d\n", len(referenced))
	fmt.Printf("Numbers protected by un-backfilled slots: %d\n", len(unbackfilledNumbers))
	fmt.Println()

	if len(candidates) == 0 {
		fmt.Println("No orphan companion rows found. Nothing to prune.")
		printPruneSummary(0, skipped, 0, !pruneDevicesApply)
		return
	}

	// Candidate table.
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NUMBER\tAD JID\tCONTAINER")
	for _, c := range candidates {
		fmt.Fprintf(w, "%s\t%s\t%s\n", c.nonAD, c.adJID, c.label)
	}
	_ = w.Flush()

	if !pruneDevicesApply {
		fmt.Printf("\n[dry-run] would delete %d orphan companion row(s). Re-run with --apply to delete.\n", len(candidates))
		printPruneSummary(len(candidates), skipped, 0, true)
		return
	}

	// --apply path.
	if !pruneDevicesYes {
		fmt.Printf("\nAbout to DELETE %d orphan companion row(s).\n", len(candidates))
		fmt.Print("The GOWA service MUST be stopped. Type 'y' to confirm: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("Aborted. Nothing deleted.")
			return
		}
	}

	deleted := 0
	for _, c := range candidates {
		if err := c.container.DeleteDevice(ctx, c.dev); err != nil {
			logrus.WithError(err).Errorf("[PRUNE] failed to delete %s from %s store", c.adJID, c.label)
			continue
		}
		deleted++
		logrus.Infof("[PRUNE] deleted %s from %s store", c.adJID, c.label)
	}
	printPruneSummary(len(candidates), skipped, deleted, false)
}

// printPruneSummary prints the final tally. In dry-run, deleted is reported as
// "would delete N"; otherwise as the actual count removed.
func printPruneSummary(candidates, skipped, deleted int, dryRun bool) {
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  candidates: %d\n", candidates)
	fmt.Printf("  skipped (un-backfilled number): %d\n", skipped)
	if dryRun {
		fmt.Printf("  would delete: %d (dry-run — pass --apply to delete)\n", candidates)
	} else {
		fmt.Printf("  deleted: %d\n", deleted)
	}
}
