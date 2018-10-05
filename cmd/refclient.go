package cmd

import (
	"github.com/buoyantio/strest-grpc/refclient"
	"github.com/spf13/cobra"
)

var refClientCmd = &cobra.Command{
	Use:   "ref-client",
	Short: "run a gRPC reference client",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		refclient.Run()
	},
}

func init() {
	RootCmd.AddCommand(refClientCmd)
}
