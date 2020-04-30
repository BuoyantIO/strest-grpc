package maxrps

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/buoyantio/strest-grpc/protos"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/gonum/optimize"
	"google.golang.org/grpc"
)

type Config struct {
	Address           string
	ConcurrencyLevels string
	TimePerLevel      time.Duration
}

// `strest-max-rps` is designed to tell you the maximum rps that
// either a strest-grpc server or an intermediary can provide. It does
// this using the Universal Scalability Law.
//
// Thanks to @brendantracey for the go playground snippet least squared regression
// code that I borrowed verbatim.
func (cfg Config) Run() {

	if cfg.TimePerLevel < time.Second {
		log.Fatalf("cfg.TimePerLevel cannot be less than 1 second.")
	}

	levels := strings.Split(cfg.ConcurrencyLevels, ",")
	var denseLatency [](float64)

	for _, l := range levels {
		level, err := strconv.Atoi(l)
		if err != nil {
			log.Fatalf("unknown concurrency level: %s, %s", l, err)
		}

		throughput := runLoadTests(&cfg.Address, level, &cfg.TimePerLevel)
		log.Debugf("%d %d", level, throughput)
		denseLatency = append(denseLatency, float64(level))
		denseLatency = append(denseLatency, float64(throughput))
	}

	latency := mat.NewDense(len(denseLatency)/2, 2, denseLatency)
	concurrency := mat.Col(nil, 0, latency)
	throughput := mat.Col(nil, 1, latency)

	// `f` and `grad` were borrowed from https://play.golang.org/p/wWUH4E5LhP
	f := func(x []float64) float64 {
		sigma, kappa, lambda := optvarsToGreek(x)
		var mismatch float64
		for i, N := range concurrency {
			pred := concurrencyToThroughput(N, sigma, kappa, lambda)
			truth := throughput[i]
			mismatch += (pred - truth) * (pred - truth)
		}
		return mismatch
	}

	grad := func(grad, x []float64) {
		for i := range grad {
			grad[i] = 0
		}
		sigma, kappa, lambda := optvarsToGreek(x)
		dSigmaDX, dKappaDX, dLambdaDX := optvarsToGreekDeriv(x)
		for i, N := range concurrency {
			pred := concurrencyToThroughput(N, sigma, kappa, lambda)
			truth := throughput[i]

			dMismatchDPred := 2 * (pred - truth)
			dPredDSigma, dPredDKappa, dPredDLambda := concurrencyToThroughputDeriv(N, sigma, kappa, lambda)

			grad[0] += dMismatchDPred * dPredDSigma * dSigmaDX
			grad[1] += dMismatchDPred * dPredDKappa * dKappaDX
			grad[2] += dMismatchDPred * dPredDLambda * dLambdaDX
		}
	}

	problem := optimize.Problem{
		Func: f,
		Grad: grad,
	}
	settings := &optimize.Settings{
		GradientThreshold: 1e-2,
		Converger: &optimize.FunctionConverge{
			Absolute:   1e-10,
			Iterations: 20,
		},
	}

	initX := []float64{0, -1, -3} // make sure they all start positive
	result, err := optimize.Minimize(problem, initX, settings, nil)
	if err != nil {
		fmt.Println("Optimization error:", err)
	}

	sigmaOpt, kappaOpt, lambdaOpt := optvarsToGreek(result.X)
	fmt.Println("sigma (the overhead of contention): ", sigmaOpt)
	fmt.Println("kappa (the overhead of crosstalk): ", kappaOpt)
	fmt.Println("lambda (unloaded performance): ", lambdaOpt)

	if log.GetLevel() >= log.DebugLevel {
		for i, v := range throughput {
			N := concurrency[i]
			pred := concurrencyToThroughput(N, sigmaOpt, kappaOpt, lambdaOpt)
			log.Debugf("true %+v pred %+v", v, pred)
		}
	}

	maxConcurrency := math.Floor(math.Sqrt((1 - sigmaOpt) / kappaOpt))
	fmt.Printf("maxConcurrency: %f\n", maxConcurrency)

	maxRps := throughputAtConcurrency(float64(maxConcurrency), kappaOpt, lambdaOpt, sigmaOpt)
	fmt.Printf("maxRps: %f\n", maxRps)
}

func throughputAtConcurrency(n, kappa, lambda, sigma float64) float64 {
	return (lambda * n) / (1 + (sigma * (n - 1)) + (kappa * n * (n - 1)))
}

// These math functions were borrowed from https://play.golang.org/p/wWUH4E5LhP
func optvarsToGreek(x []float64) (sigma, kappa, lambda float64) {
	return math.Exp(x[0]), math.Exp(x[1]), math.Exp(x[2])
}

func optvarsToGreekDeriv(x []float64) (dSigmaDX, dKappaDX, dLambdaDX float64) {
	return math.Exp(x[0]), math.Exp(x[1]), math.Exp(x[2])
}

func concurrencyToThroughput(concurrency, sigma, kappa, lambda float64) float64 {
	N := concurrency
	return lambda * N / (1 + sigma*(N-1) + kappa*N*(N-1))
}

func concurrencyToThroughputDeriv(concurrency, sigma, kappa, lambda float64) (dSigma, dKappa, dLambda float64) {
	// X(N) = lambda * N / (1 + sigma*(N-1) + kappa*N*(N-1))
	N := concurrency
	num := lambda * N
	denom := 1 + sigma*(N-1) + kappa*N*(N-1)
	dSigma = -(num / (denom * denom)) * (N - 1)
	dKappa = -(num / (denom * denom)) * (N - 1) * N
	dLambda = N / denom
	return dSigma, dKappa, dLambda
}

// Converts a slice of chan int to a slice of int.
func chansToSlice(cs []<-chan int, size int) []int {
	s := make([]int, size)
	for i, c := range cs {
		for m := range c {
			s[i] = m
		}
	}
	return s
}

// Runs a single load test, returns how many requests were sent in a second.
func runLoadTest(client pb.ResponderClient, wg *sync.WaitGroup, startWg *sync.WaitGroup, timePerLevel *time.Duration) <-chan int {
	out := make(chan int, 1)
	go func() {
		defer wg.Done()
		// Roughly synchronize the start of all our load test goroutines
		startWg.Wait()
		start := time.Now()
		requests := 0
		for ; time.Now().Sub(start) <= *timePerLevel; requests++ {
			_, err := client.Get(context.Background(),
				&pb.ResponseSpec{
					Length:  0,
					Latency: 0,
				})

			if err != nil {
				// TODO: have an err channel so we can report the # of errs
				log.Printf("Error issuing request %e", err)
				continue
			}
		}
		rps := requests / int(timePerLevel.Seconds())
		out <- rps
		close(out)
	}()

	return out
}

// returns how many requests were sent in one second at concurrencyLevel
func runLoadTests(address *string, concurrencyLevel int, timePerLevel *time.Duration) int {
	var wg sync.WaitGroup
	var startWg sync.WaitGroup
	// a slice of channels containing throughput per goroutine
	var requests []<-chan int
	startWg.Add(1)
	wg.Add(concurrencyLevel)

	for i := 0; i < concurrencyLevel; i++ {
		conn, err := grpc.Dial(*address, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("did not connect: %v", err)
		}
		defer conn.Close()
		client := pb.NewResponderClient(conn)
		request := runLoadTest(client, &wg, &startWg, timePerLevel)
		requests = append(requests, request)
	}

	startWg.Done()
	wg.Wait()
	requestsPerWorker := chansToSlice(requests, concurrencyLevel)
	totalRequests := 0
	for _, requests := range requestsPerWorker {
		totalRequests += requests
	}

	return totalRequests
}
