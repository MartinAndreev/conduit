package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/MartinAndreev/conduit/internal/app/startup"
	"github.com/MartinAndreev/conduit/internal/presentation/i18n"
	"github.com/MartinAndreev/conduit/internal/startupinfra"
	"github.com/MartinAndreev/conduit/internal/tui"
)

func main() {
	language := flag.String("lang", "en", "interface language: en or bg")
	version := flag.Bool("version", false, "print the development version")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Conduit — terminal application")
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: conduit [--help] [--version]")
		fmt.Fprintln(flag.CommandLine.Output(), "Run without flags to open the terminal application.")
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
	if err := tui.RunLocale(startup.Service{Repository: startupinfra.Files{}, Detector: startupinfra.Detector{}}, i18n.Normalize(*language)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
