package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	version := flag.Bool("version", false, "print the development version")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Conduit Go rewrite — project scaffold")
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: conduit [--help] [--version]")
		fmt.Fprintln(flag.CommandLine.Output(), "Orchestration commands are not implemented yet.")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "unsupported command: %s\n", flag.Arg(0))
		os.Exit(2)
	}
	if *version {
		fmt.Println("conduit go-rewrite-a (development scaffold)")
		return
	}
	flag.Usage()
}
