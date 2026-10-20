// metallic-doctor analyzes a bounded local log excerpt without sending it to a
// service or changing files. A successful analysis is not a health certificate.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/oncoursemail/metallic-mc/internal/diagnostics"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("metallic-doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("log", "", "local log file (otherwise read standard input)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: metallic-doctor [-log path]\nAnalyzes up to 1 MiB of local Minecraft/JVM logs. No upload, commands, or automatic fixes.\nOutput contains fixed findings and evidence line numbers, never raw log excerpts.")
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return 2
	}
	input := stdin
	if *path != "" {
		f, err := os.Open(*path)
		if err != nil {
			fmt.Fprintln(stderr, "Could not open the local log file.")
			return 2
		}
		defer f.Close()
		input = f
	}
	data, err := io.ReadAll(io.LimitReader(input, diagnostics.MaxLogBytes+1))
	if err != nil {
		fmt.Fprintln(stderr, "Could not read the local log excerpt.")
		return 2
	}
	report, err := diagnostics.Analyze(diagnostics.Request{Log: string(data)})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err = enc.Encode(report); err != nil {
		fmt.Fprintln(stderr, "Could not write the diagnosis report.")
		return 2
	}
	return 0
}
