package cmd

import (
	"github.com/buoyantio/strest-grpc/server"
	"github.com/spf13/cobra"
)

var serverCfg = server.Config{}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "run the strest-grpc server",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		serverCfg.Run()
	},
}

func init() {
	RootCmd.AddCommand(serverCmd)
	flags := serverCmd.Flags()
	flags.StringVar(&serverCfg.Address, "address", ":11111", "address to serve on")
	flags.StringVar(&serverCfg.MetricAddr, "metricAddr", "", "address to serve metrics on")
	flags.StringVar(&serverCfg.LatencyPercentiles, "latencyPercentiles", "100=0", "response latency percentile distribution added to client latencies. (e.g. 50=10,100=100)")
	flags.BoolVarP(&serverCfg.UseUnixAddr, "unix", "u", false, "use Unix Domain Sockets instead of TCP")
	flags.StringVar(&serverCfg.TLSCertFile, "tlsCertFile", "", "the path to the trust certificate")
	flags.StringVar(&serverCfg.TLSPrivKeyFile, "tlsPrivKeyFile", "", "the path to the server's private key")
}
