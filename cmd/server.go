package cmd

import (
	"github.com/buoyantio/strest-grpc/server"
	"github.com/spf13/cobra"
)

// TODO: this is horrible. make a struct.
var (
	serverAddress     string
	serverUseUnixAddr bool
	serverMetricAddr  string
	tlsCertFile       string
	tlsPrivKeyFile    string
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "run the strest-grpc server",
	Run: func(cmd *cobra.Command, args []string) {
		server.Run(
			&address, &useUnixAddr, &metricAddr, &tlsCertFile, &tlsPrivKeyFile)
	},
	Args: cobra.NoArgs,
}

func init() {
	RootCmd.AddCommand(serverCmd)
	flags := serverCmd.PersistentFlags()
	flags.StringVar(&serverAddress, "address", ":11111", "address to serve on")
	flags.BoolVar(&serverUseUnixAddr, "unix", false, "use Unix Domain Sockets instead of TCP")
	flags.StringVar(&tlsCertFile, "tlsCertFile", "", "the path to the trust certificate")
	flags.StringVar(&tlsPrivKeyFile, "tlsPrivKeyFile", "", "the path to the server's private key")
}
