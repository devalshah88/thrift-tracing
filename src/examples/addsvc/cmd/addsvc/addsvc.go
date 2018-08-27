package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/devalshah88/thrift/lib/go/thrift"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkin "github.com/openzipkin/zipkin-go-opentracing"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"examples/addsvc/pkg/addendpoint"

	addthrift "examples/addsvc/thrift/gen-go/addsvc"
	thrifttracing "tracing"

	"examples/addsvc/pkg/addservice"

	"examples/addsvc/pkg/addtransport"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/prometheus"
	"github.com/oklog/oklog/pkg/group"
)

func main() {
	fs := flag.NewFlagSet("addsvc", flag.ExitOnError)
	var (
		debugAddr        = fs.String("debug.addr", ":8080", "Debug and metrics listen address")
		thriftAddr       = fs.String("thrift-addr", ":8083", "Thrift listen address")
		thriftBuffer     = fs.Int("thrift-buffer", 8192, "0 for unbuffered")
		zipkinAddr       = fs.String("zipkin-addr", "", "Zipkin HTTP Collector endpoint")
		zipkinSampleRate = fs.Float64("zipkin-sampling-rate", 1.0, "Zipkin Sampling")
	)
	fs.Usage = usageFor(fs, os.Args[0]+" [flags]")
	fs.Parse(os.Args[1:])

	// Create a single logger, which we'll use and give to other components.
	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}

	var tracer stdopentracing.Tracer
	{

		if *zipkinAddr != "" {
			log.With(logger, "tracer", "ZipkinHTTP").Log()

			collector, err := zipkin.NewHTTPCollector(*zipkinAddr)
			if err != nil {
				logger.Log("err", err)
				os.Exit(1)
			}
			defer collector.Close()

			tracer, err = zipkin.NewTracer(
				zipkin.NewRecorder(collector, true, "0.0.0.0:0", "addsvc"),
				zipkin.TraceID128Bit(true),
				zipkin.ClientServerSameSpan(true),
				zipkin.WithSampler(zipkin.NewBoundarySampler(*zipkinSampleRate, 8675309)),
			)
			if err != nil {
				logger.Log("err", err)
				os.Exit(1)
			}
		} else {
			tracer = stdopentracing.GlobalTracer() // no-op
		}
	}

	var ints, chars metrics.Counter
	{
		// Business-level metrics.
		ints = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: "example",
			Subsystem: "addsvc",
			Name:      "integers_summed",
			Help:      "Total count of integers summed via the Sum method.",
		}, []string{})
		chars = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: "example",
			Subsystem: "addsvc",
			Name:      "characters_concatenated",
			Help:      "Total count of characters concatenated via the Concat method.",
		}, []string{})
	}
	var duration metrics.Histogram
	{
		// Endpoint-level metrics.
		duration = prometheus.NewSummaryFrom(stdprometheus.SummaryOpts{
			Namespace: "example",
			Subsystem: "addsvc",
			Name:      "request_duration_seconds",
			Help:      "Request duration in seconds.",
		}, []string{"method", "success"})
	}
	http.DefaultServeMux.Handle("/metrics", promhttp.Handler())

	// Build the layers of the service "onion" from the inside out. First, the
	// business logic service; then, the set of endpoints that wrap the service;
	// and finally, a series of concrete transport adapters. The adapters, like
	// the HTTP handler or the gRPC server, are the bridge between Go kit and
	// the interfaces that the transports expect. Note that we're not binding
	// them to ports or anything yet; we'll do that next.
	var (
		service      = addservice.New(logger, ints, chars)
		endpoints    = addendpoint.New(service, logger, duration, tracer)
		thriftServer = addtransport.NewThriftServer(endpoints)
	)

	// Now we're to the part of the func main where we want to start actually
	// running things, like servers bound to listeners to receive connections.
	//
	// The method is the same for each component: add a new actor to the group
	// struct, which is a combination of 2 anonymous functions: the first
	// function actually runs the component, and the second function should
	// interrupt the first function and cause it to return. It's in these
	// functions that we actually bind the Go kit server/handler structs to the
	// concrete transports and run them.
	//
	// Putting each component into its own block is mostly for aesthetics: it
	// clearly demarcates the scope in which each listener/socket may be used.
	var g group.Group
	{
		// The debug listener mounts the http.DefaultServeMux, and serves up
		// stuff like the Prometheus metrics route, the Go debug and profiling
		// routes, and so on.
		debugListener, err := net.Listen("tcp", *debugAddr)
		if err != nil {
			logger.Log("transport", "debug/HTTP", "during", "Listen", "err", err)
			os.Exit(1)
		}
		g.Add(func() error {
			logger.Log("transport", "debug/HTTP", "addr", *debugAddr)
			return http.Serve(debugListener, http.DefaultServeMux)
		}, func(error) {
			debugListener.Close()
		})
	}

	{
		// The Thrift socket mounts the Go kit Thrift server we created earlier.
		// There's a lot of boilerplate involved here, related to configuring
		// the protocol and transport; blame Thrift.
		thriftSocket, err := thrift.NewTServerSocket(*thriftAddr)
		if err != nil {
			logger.Log("transport", "Thrift", "during", "Listen", "err", err)
			os.Exit(1)
		}
		g.Add(func() error {
			logger.Log("transport", "Thrift", "addr", *thriftAddr)
			protocolFactory := thrift.NewTHeaderProtocolFactory()
			transportFactory := thrift.NewTHeaderTransportFactory(thrift.NewTBufferedTransportFactory(*thriftBuffer))

			return thrift.NewTSimpleServer4(
				thrifttracing.MergeAndWrap(tracer, logger, addthrift.NewAddServiceProcessor(thriftServer)),
				thriftSocket,
				transportFactory,
				protocolFactory,
			).Serve()
		}, func(error) {
			thriftSocket.Close()
		})
	}
	{
		// This function just sits and waits for ctrl-C.
		cancelInterrupt := make(chan struct{})
		g.Add(func() error {
			c := make(chan os.Signal, 1)
			signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
			select {
			case sig := <-c:
				return fmt.Errorf("received signal %s", sig)
			case <-cancelInterrupt:
				return nil
			}
		}, func(error) {
			close(cancelInterrupt)
		})
	}
	logger.Log("exit", g.Run())
}

func usageFor(fs *flag.FlagSet, short string) func() {
	return func() {
		fmt.Fprintf(os.Stderr, "USAGE\n")
		fmt.Fprintf(os.Stderr, "  %s\n", short)
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "FLAGS\n")
		w := tabwriter.NewWriter(os.Stderr, 0, 2, 2, ' ', 0)
		fs.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(w, "\t-%s %s\t%s\n", f.Name, f.DefValue, f.Usage)
		})
		w.Flush()
		fmt.Fprintf(os.Stderr, "\n")
	}
}
