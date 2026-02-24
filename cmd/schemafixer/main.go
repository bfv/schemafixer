package main

import (
	"os"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X main.version=x.y.z".
var version = "dev"

func main() {
	var verbose bool

	rootCmd := &cobra.Command{
		Use:     "schemafixer",
		Short:   "Fix OpenEdge .df schema file area assignments",
		Version: version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			initLogging(verbose)
		},
	}

	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose (debug) logging")
	rootCmd.AddCommand(newApplyCmd())
	rootCmd.AddCommand(newParseCmd())

	if err := rootCmd.Execute(); err != nil {
		log.Error().Err(err).Msg("fatal error")
		os.Exit(1)
	}
}
