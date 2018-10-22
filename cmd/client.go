package cmd

import (
	"time"

	"github.com/buoyantio/strest-grpc/client"
	"github.com/spf13/cobra"
)

var clientCfg = client.Config{}

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "run the strest-grpc client",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		clientCfg.Run()
	},
}

func init() {
	RootCmd.AddCommand(clientCmd)
	flags := clientCmd.Flags()
	flags.StringVar(&clientCfg.Address, "address", "localhost:11111", "address of strest-grpc service or intermediary")
	flags.BoolVarP(&clientCfg.UseUnixAddr, "unix", "u", false, "use Unix Domain Sockets instead of TCP")
	flags.DurationVar(&clientCfg.ClientTimeout, "clientTimeout", 0, "timeout for unary client requests. Default: no timeout")
	flags.UintVar(&clientCfg.Connections, "connections", 1, "number of concurrent connections")
	flags.UintVar(&clientCfg.Streams, "streams", 1, "number of concurrent streams per connection")
	flags.UintVar(&clientCfg.TotalRequests, "totalRequests", 0, "total number of requests to send. default: infinite")
	flags.UintVar(&clientCfg.TotalTargetRps, "totalTargetRps", 0, "target requests per second")
	flags.DurationVar(&clientCfg.Interval, "interval", 10*time.Second, "reporting interval")
	flags.UintVar(&clientCfg.NumIterations, "iterations", 0, "Number of iterations (0 for infinite)")
	flags.StringVar(&clientCfg.LatencyPercentiles, "latencyPercentiles", "100=0", "response latency percentile distribution (in ms). (e.g. 50=10,100=100)")
	flags.StringVar(&clientCfg.LengthPercentiles, "lengthPercentiles", "100=0", "response body length percentile distribution. (e.g. 50=100,100=1000)")
	flags.Float64Var(&clientCfg.ErrorRate, "errorRate", 0.0, "the chance to return an error")
	flags.BoolVar(&clientCfg.NoFinalReport, "noFinalReport", false, "do not print a final JSON output report")
	flags.BoolVar(&clientCfg.NoIntervalReport, "noIntervalReport", false, "only print the final report, nothing intermediate")
	flags.BoolVar(&clientCfg.Streaming, "streaming", false, "use the streaming features of strest server")
	flags.StringVar(&clientCfg.StreamingRatio, "streamingRatio", "1:1", "the ratio of streaming requests/responses")
	flags.StringVar(&clientCfg.MetricAddr, "metricAddr", "", "address to serve metrics on")
	flags.StringVar(&clientCfg.LatencyUnit, "latencyUnit", "ms", "latency units [ms|us|ns]")
	flags.StringVar(&clientCfg.TlsTrustChainFile, "tlsTrustChainFile", "", "the path to the certificate used to validate the remote's signature")
}
