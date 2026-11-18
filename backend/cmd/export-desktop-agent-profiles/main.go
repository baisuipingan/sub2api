// Export the reviewed built-in catalogue for the desktop profile release feed.
// This command reads no settings, credentials, or upstream model endpoints.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func main() {
	outputPath := flag.String("output", "-", "JSON destination, or - for stdout")
	flag.Parse()
	profiles, err := new(service.DesktopAgentService).GetProfiles(context.Background())
	if err != nil {
		fail(err)
	}
	var revision string
	for _, profile := range profiles {
		if profile.Revision == "" {
			fail(fmt.Errorf("profile %s has no reviewed revision", profile.ID))
		}
		revision = max(revision, profile.Revision)
	}
	envelope := struct {
		SchemaVersion int                           `json:"schema_version"`
		Revision      string                        `json:"revision"`
		Profiles      []service.DesktopAgentProfile `json:"profiles"`
	}{SchemaVersion: 1, Revision: revision, Profiles: profiles}
	raw, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		fail(err)
	}
	raw = append(raw, '\n')
	if *outputPath == "-" {
		if _, err := os.Stdout.Write(raw); err != nil {
			fail(err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(*outputPath), 0755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(*outputPath, raw, 0644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
