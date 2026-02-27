package main

import (
	"os"
	"runtime/debug"

	"github.com/bfv/schemafixer/cmd/schemafixer/commands"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X main.version=x.y.z".
// If not set (e.g., via go install), it will be determined from build info.
var version = "dev"

func init() {
	// If version is still "dev", try to get it from build info (for go install)
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}
}

func main() {
	var verbose bool

	rootCmd := &cobra.Command{
		Use:     "schemafixer",
		Short:   "Fix OpenEdge .df schema file area assignments",
		Version: version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			commands.InitLogging(verbose)
		},
	}

	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose (debug) logging")
	rootCmd.AddCommand(commands.NewApplyCmd())
	rootCmd.AddCommand(commands.NewParseCmd())
	rootCmd.AddCommand(commands.NewDiffCmd())

	if err := rootCmd.Execute(); err != nil {
		log.Error().Err(err).Msg("fatal error")
		os.Exit(1)
	}
}
