// Command schemadiff compares two `terraform providers schema -json` documents, usually
// the previous release's and the current build's, and lists added, removed and changed
// attributes. Breaking changes are marked BREAK; they make a major release (ADR D4).
//
//	schemadiff [-fail-on-breaking] old.json new.json
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	failOnBreaking := flag.Bool("fail-on-breaking", false, "exit with status 2 when a change is breaking")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: schemadiff [-fail-on-breaking] old.json new.json")
		os.Exit(64)
	}
	load := func(path string) *Schemas {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "schemadiff:", err)
			os.Exit(1)
		}
		s, err := ParseSchemas(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "schemadiff: %s: %v\n", path, err)
			os.Exit(1)
		}
		return s
	}
	changes := Diff(load(flag.Arg(0)), load(flag.Arg(1)))
	for _, c := range changes {
		fmt.Println(c)
	}
	if len(changes) == 0 {
		fmt.Println("no schema changes")
	}
	if *failOnBreaking && HasBreaking(changes) {
		os.Exit(2)
	}
}
