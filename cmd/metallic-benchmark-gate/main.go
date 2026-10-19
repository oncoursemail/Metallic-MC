// metallic-benchmark-gate performs offline evidence screening only.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/oncoursemail/metallic-mc/internal/benchgate"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, time.Now().UTC())) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, now time.Time) int {
	flags := flag.NewFlagSet("metallic-benchmark-gate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "-", "JSON evidence file, or - for stdin (maximum 512 KiB)")
	root := flags.String("artifact-root", "", "explicit directory containing immutable local traces; required for screening")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	reader := stdin
	if *input != "-" {
		f, err := os.Open(*input)
		if err != nil {
			fmt.Fprintln(stderr, "cannot open evidence file:", err)
			return 2
		}
		defer f.Close()
		reader = f
	}
	report, err := benchgate.Decode(reader)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	result := benchgate.Screen(report, *root, now)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(stderr, "cannot encode screening result:", err)
		return 2
	}
	if result.Status == "passed_screening" {
		return 0
	}
	return 1
}
