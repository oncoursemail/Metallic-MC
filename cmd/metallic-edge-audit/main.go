// metallic-edge-audit performs a local-only configuration/evidence preflight.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/edgeaudit"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now())) }

func run(args []string, out, errOut io.Writer, now time.Time) int {
	flags := flag.NewFlagSet("metallic-edge-audit", flag.ContinueOnError)
	flags.SetOutput(errOut)
	configPath := flags.String("config", "", "local JSON configuration file (required)")
	evidenceDir := flags.String("evidence-dir", "", "folder containing local evidence artifacts; never downloaded")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *configPath == "" || flags.NArg() != 0 {
		fmt.Fprintln(errOut, "usage: metallic-edge-audit -config manifest.json [-evidence-dir folder]")
		return 2
	}
	before, err := os.Lstat(*configPath)
	if err != nil || !before.Mode().IsRegular() || before.Size() > edgeaudit.MaxConfigBytes {
		fmt.Fprintln(errOut, "configuration must be a regular file of at most 65536 bytes")
		return 2
	}
	f, err := os.Open(*configPath)
	if err != nil {
		fmt.Fprintln(errOut, "cannot open configuration")
		return 2
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		fmt.Fprintln(errOut, "configuration changed while opening")
		return 2
	}
	config, digest, err := edgeaudit.ReadConfig(f)
	if err != nil {
		fmt.Fprintf(errOut, "invalid configuration: %v\n", err)
		return 2
	}
	var artifacts edgeaudit.ArtifactReader
	if *evidenceDir != "" {
		artifacts = edgeaudit.DirectoryArtifacts{Directory: *evidenceDir}
	}
	report := edgeaudit.Audit(config, now, artifacts)
	report.ConfigurationSHA256 = digest
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintln(errOut, "cannot write audit report")
		return 2
	}
	if report.Status != "configuration_consistent" {
		return 1
	}
	return 0
}
