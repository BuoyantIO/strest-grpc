package cmd

import (
	"github.com/buoyantio/strest-grpc/refserver"
	"github.com/spf13/cobra"
)

var refServerCmd = &cobra.Command{
	Use:   "ref-server",
	Short: "run a gRPC reference server",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		refserver.Run()
	},
}

func init() {
	RootCmd.AddCommand(refServerCmd)
}
