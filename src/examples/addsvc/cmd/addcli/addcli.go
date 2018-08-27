package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"examples/addsvc/pkg/addservice"
	"examples/addsvc/pkg/addtransport"

	addthrift "examples/addsvc/thrift/gen-go/addsvc"
	thrifttracing "tracing"

	"github.com/devalshah88/thrift/lib/go/thrift"
	"github.com/go-kit/kit/log"
	stdopentracing "github.com/opentracing/opentracing-go"
	zipkin "github.com/openzipkin/zipkin-go-opentracing"
)

func main() {
	fs := flag.NewFlagSet("addcli", flag.ExitOnError)
	var (
		thriftAddr       = fs.String("thrift-addr", "", "Thrift address of addsvc")
		thriftBuffer     = fs.Int("thrift-buffer", 8192, "buffer size")
		method           = fs.String("method", "sum", "sum, concat")
		zipkinAddr       = fs.String("zipkin-addr", "", "Zipkin HTTP Collector endpoint")
		zipkinSampleRate = fs.Float64("zipkin-sampling-rate", 1.0, "Zipkin Sampling")
	)
	fs.Usage = usageFor(fs, os.Args[0]+" [flags] <a> <b>")
	fs.Parse(os.Args[1:])
	if len(fs.Args()) != 2 {
		fs.Usage()
		os.Exit(1)
	}
	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}

	var otTracer stdopentracing.Tracer
	{
		{

			if *zipkinAddr != "" {
				log.With(logger, "tracer", "ZipkinHTTP").Log()

				collector, err := zipkin.NewHTTPCollector(*zipkinAddr)
				if err != nil {
					logger.Log("err", err)
					os.Exit(1)
				}
				defer collector.Close()

				otTracer, err = zipkin.NewTracer(
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
				otTracer = stdopentracing.GlobalTracer() // no-op
			}

		}
	}

	var (
		svc addservice.Service
		err error
	)

	if *thriftAddr != "" {
		protocolFactory := thrift.NewTHeaderProtocolFactory()
		transportFactory := thrift.NewTHeaderTransportFactory(thrift.NewTBufferedTransportFactory(*thriftBuffer))

		transportSocket, err := thrift.NewTSocket(*thriftAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		transport, err := transportFactory.GetTransport(transportSocket)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if err := transport.Open(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		defer transport.Close()
		tracingClient := thrifttracing.NewTracingThriftClient(protocolFactory.GetProtocol(transport), protocolFactory.GetProtocol(transport), logger, otTracer)
		client := addthrift.NewAddServiceClient(tracingClient)
		svc = addtransport.NewThriftClient(client)
	} else {
		fmt.Fprintf(os.Stderr, "error: no remote address specified\n")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	switch *method {
	case "sum":
		a, _ := strconv.ParseInt(fs.Args()[0], 10, 64)
		b, _ := strconv.ParseInt(fs.Args()[1], 10, 64)
		v, err := svc.Sum(context.Background(), int(a), int(b))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "%d + %d = %d\n", a, b, v)

	case "concat":
		a := fs.Args()[0]
		b := fs.Args()[1]
		v, err := svc.Concat(context.Background(), a, b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "%q + %q = %q\n", a, b, v)

	default:
		fmt.Fprintf(os.Stderr, "error: invalid method %q\n", *method)
		os.Exit(1)
	}
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
