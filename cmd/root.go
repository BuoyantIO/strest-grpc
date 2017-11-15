package cmd

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var logLevel string
var logLevelMsg = "must be one of: panic, fatal, error, warn, info, debug"

var RootCmd = &cobra.Command{
	Use:   "strest-grpc [client | server | max-rps]",
	Short: "A load tester for stress testing grpc intermediaries.",
	Long: `A load tester for stress testing grpc intermediaries.

Find more information at https://github.com/buoyantio/strest-grpc.`,
	Args: cobra.NoArgs,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// TODO: share other flags here, such as address
		level, err := log.ParseLevel(logLevel)
		if err != nil {
			log.Fatalf("invalid logLevel: %s, %s", logLevel, logLevelMsg)
		}
		log.SetLevel(level)
	},
}

// init adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func init() {
	RootCmd.PersistentFlags().StringVarP(&logLevel, "logLevel", "l", "info", "log level, "+logLevelMsg)
}
