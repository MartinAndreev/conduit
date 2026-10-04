package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// This helper enters the actual CLI parser, including its help exit, without
// launching plain Conduit or any real harness. Output proves it stayed on the
// informational path rather than entering the terminal application.
func TestInformationalCLI(t *testing.T) {
	if os.Getenv("CONDUIT_CLI_HELPER") == "1" {
		arguments := strings.Split(os.Getenv("CONDUIT_CLI_ARGUMENTS"), " ")
		os.Args = append([]string{"conduit"}, arguments...)
		flag.CommandLine = flag.NewFlagSet("conduit", flag.ExitOnError)
		flag.CommandLine.Usage = func() { flag.Usage() }
		main()
		return
	}
	for _, fixture := range []struct{ arguments, expected string }{{"--lang de --help", "Run without flags"}, {"--lang bg --version", "go-rewrite-a"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInformationalCLI$")
		command.Env = append(os.Environ(), "CONDUIT_CLI_HELPER=1", "CONDUIT_CLI_ARGUMENTS="+fixture.arguments)
		output, err := command.CombinedOutput()
		cancel()
		if err != nil || !strings.Contains(string(output), fixture.expected) {
			t.Fatalf("%s: %v, %s", fixture.arguments, err, output)
		}
	}
}
