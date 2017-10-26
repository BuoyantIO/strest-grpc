package cmd

import (
	"time"

	"github.com/buoyantio/strest-grpc/client"
	"github.com/spf13/cobra"
)

var (
	address            string
	useUnixAddr        bool
	clientTimeout      time.Duration
	connections        uint
	streams            uint
	totalRequests      uint
	totalTargetRps     uint
	interval           time.Duration
	latencyPercentiles string
	lengthPercentiles  string
	errorRate          float64
	noFinalReport      bool
	noIntervalReport   bool
	streaming          bool
	streamingRatio     string
	metricAddr         string
	latencyUnit        string
	tlsTrustChainFile  string
)

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "run the strest-grpc client",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		client.Run(address,
			useUnixAddr,
			clientTimeout,
			connections,
			streams,
			totalRequests,
			totalTargetRps,
			interval,
			latencyPercentiles,
			lengthPercentiles,
			errorRate,
			noFinalReport,
			noIntervalReport,
			streaming,
			streamingRatio,
			metricAddr,
			latencyUnit,
			tlsTrustChainFile)
	},
}

func init() {
	RootCmd.AddCommand(clientCmd)
	flags := clientCmd.PersistentFlags()
	flags.StringVar(&address, "address", "localhost:1111", "address of strest-grpc service or intermediary")
	flags.BoolVar(&useUnixAddr, "unix", false, "use Unix Domain Sockets instead of TCP")
	flags.DurationVar(&clientTimeout, "clientTimeout", 0, "timeout for unary client requests. Default: no timeout")
	flags.UintVar(&connections, "connections", 1, "number of concurrent connections")
	flags.UintVar(&streams, "streams", 1, "number of concurrent streams per connection")
	flags.UintVar(&totalRequests, "totalRequests", 0, "total number of requests to send. default: infinite")
	flags.UintVar(&totalTargetRps, "totalTargetRps", 0, "target requests per second")
	flags.DurationVar(&interval, "interval", 10*time.Second, "reporting interval")
	flags.StringVar(&latencyPercentiles, "latencyPercentiles", "100=0", "response latency percentile distribution. (e.g. 50=10,100=100)")
	flags.StringVar(&lengthPercentiles, "lengthPercentiles", "100=0", "response body length percentile distribution. (e.g. 50=100,100=1000)")
	flags.Float64Var(&errorRate, "errorRate", 0.0, "the chance to return an error")
	flags.BoolVar(&noFinalReport, "noFinalReport", false, "do not print a final JSON output report")
	flags.BoolVar(&noIntervalReport, "noIntervalReport", false, "only print the final report, nothing intermediate")
	flags.BoolVar(&streaming, "streaming", false, "use the streaming features of strest server")
	flags.StringVar(&streamingRatio, "streamingRatio", "1:1", "the ratio of streaming requests/responses")
	flags.StringVar(&metricAddr, "metricAddr", "", "address to serve metrics on")
	flags.StringVar(&latencyUnit, "latencyUnit", "ms", "latency units [ms|us|ns]")
	flags.StringVar(&tlsTrustChainFile, "tlsTrustChainFile", "", "the path to the certificate used to validate the remote's signature")

}
