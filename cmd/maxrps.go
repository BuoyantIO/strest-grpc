package cmd

import (
	"flag"
	"time"

	maxrps "github.com/buoyantio/strest-grpc/max-rps"
	"github.com/spf13/cobra"
)

var cfg = maxrps.Config{}

var maxrpsCmd = &cobra.Command{
	Use:   "max-rps",
	Short: "compute max RPS",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		cfg.Run()
	},
}

func init() {
	RootCmd.AddCommand(maxrpsCmd)
	flags := maxrpsCmd.Flags()
	flags.StringVar(&cfg.Address, "address", "localhost:11111", "hostname:port of strest-grpc service or intermediary")
	flags.StringVar(&cfg.ConcurrencyLevels, "concurrencyLevels", "1,5,10,20,30", "levels of concurrency to test with")
	flags.DurationVar(&cfg.TimePerLevel, "timePerLevel", 1*time.Second, "how much time to spend testing each concurrency level")
	flag.BoolVar(&cfg.Debug, "debug", false, "print out some extra information for debugging")
}
