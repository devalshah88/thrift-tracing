package tracing

/* This file contains code that wraps a thrift Processor with a function that
   extracts trace identifiers from the request headers and injects them into the context object
   To use it, simply do something like below in the server set up code :

   thrift.NewTSimpleServer4(
				MergeAndWrap(tracer, logger, addthrift.NewAddServiceProcessor(thriftServer)),
				thriftSocket,
				transportFactory,
				protocolFactory)

    For a better example, see examples/addsvc
*/
import (
	"context"

	"github.com/devalshah88/thrift/lib/go/thrift"
	"github.com/go-kit/kit/log"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
)

// mutableTProcessor interface contains the functions needed to merge processors together
// It is used by the MergeAndWrap function
type mutableTProcessor interface {
	Process(ctx context.Context, in, out thrift.TProtocol) (bool, thrift.TException)
	AddToProcessorMap(key string, processor thrift.TProcessorFunction)
	ProcessorMap() map[string]thrift.TProcessorFunction
}

// MergeAndWrap merges multiple processors together and Wraps each TProcessorFunction
// object with a wrapper that can inject header data into the context object
func MergeAndWrap(tracer opentracing.Tracer, logger log.Logger, processors ...mutableTProcessor) thrift.TProcessor {
	firstProcessor := processors[0]
	for k, v := range firstProcessor.ProcessorMap() {
		firstProcessor.AddToProcessorMap(k, wrapProcessorFunction(v, k, tracer, logger))
	}
	for i := 1; i < len(processors); i++ {
		nextProcessor := processors[i]
		for k, v := range nextProcessor.ProcessorMap() {
			firstProcessor.AddToProcessorMap(k, wrapProcessorFunction(v, k, tracer, logger))
		}
	}
	return firstProcessor
}

func wrapProcessorFunction(p thrift.TProcessorFunction, n string, t opentracing.Tracer, l log.Logger) thrift.TProcessorFunction {
	return wrappedProcessorFunction{p, n, t, l}
}

// wrappedProcessorFunction implements the thrift.TProcessorFunction interface
// It extracts headers from the input protocol object and injects them into the
// Context object
type wrappedProcessorFunction struct {
	processor thrift.TProcessorFunction
	name      string
	tracer    opentracing.Tracer
	logger    log.Logger
}

func (w wrappedProcessorFunction) Process(ctx context.Context, seqId int32, iprot, oprot thrift.TProtocol) (success bool, err thrift.TException) {
	// Extract headers into context object
	headerProto, ok := iprot.(*thrift.THeaderProtocol)
	if ok {
		var span opentracing.Span
		wireContext, err := w.tracer.Extract(opentracing.TextMap, THeaderReaderWriter{headerProto})
		if err != nil && err != opentracing.ErrSpanContextNotFound {
			w.logger.Log("err", err)
		}
		span = w.tracer.StartSpan(w.name, ext.RPCServerOption(wireContext))
		ctx = opentracing.ContextWithSpan(ctx, span)
	}
	return w.processor.Process(ctx, seqId, iprot, oprot)
}
