package cmd

import (
	"github.com/buoyantio/strest-grpc/server"
	"github.com/spf13/cobra"
)

var serverCfg = server.Config{}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "run the strest-grpc server",
	Run: func(cmd *cobra.Command, args []string) {
		serverCfg.Run()
	},
	Args: cobra.NoArgs,
}

func init() {
	RootCmd.AddCommand(serverCmd)
	flags := serverCmd.PersistentFlags()
	flags.StringVar(&serverCfg.Address, "address", ":11111", "address to serve on")
	flags.StringVar(&serverCfg.MetricAddr, "metricAddr", "", "address to serve metrics on")
	flags.BoolVarP(&serverCfg.UseUnixAddr, "unix", "u", false, "use Unix Domain Sockets instead of TCP")
	flags.StringVar(&serverCfg.TLSCertFile, "tlsCertFile", "", "the path to the trust certificate")
	flags.StringVar(&serverCfg.TLSPrivKeyFile, "tlsPrivKeyFile", "", "the path to the server's private key")
}
