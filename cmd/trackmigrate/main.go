package main

import (
	"flag"
	"fmt"
	"os"

	"empirebus-tests/service/tracking"
)

func main() {
	dir := flag.String("dir", "/var/lib/xtura/tracks", "track directory")
	dryRun := flag.Bool("dry-run", false, "report changes without writing files")
	flag.Parse()
	report, err := tracking.MigrateLegacyTracks(*dir, *dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	action := "Migrated"
	if *dryRun {
		action = "Would migrate"
	}
	fmt.Printf("%s %d day(s), %d legacy file(s), %d point(s)\n", action, report.Days, report.Files, report.Points)
}
