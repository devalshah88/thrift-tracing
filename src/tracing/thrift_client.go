package tracing

import (
	"context"

	"github.com/go-kit/kit/log"

	"github.com/devalshah88/thrift/lib/go/thrift"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
)

// TracingThriftClient implements the TClient interface
// It acts as a wrapper around the TStandardClient and is responsible
// for injecting trace identifiers from the context object to the protocol
type TracingThriftClient struct {
	Client       thrift.TClient
	iprot, oprot thrift.TProtocol
	logger       log.Logger
	tracer       opentracing.Tracer
}

func NewTracingThriftClient(inputProtocol, outputProtocol thrift.TProtocol, logger log.Logger, tracer opentracing.Tracer) *TracingThriftClient {
	return &TracingThriftClient{Client: thrift.NewTStandardClient(inputProtocol, outputProtocol), iprot: inputProtocol, oprot: outputProtocol, logger: logger, tracer: tracer}
}

func (p *TracingThriftClient) Call(ctx context.Context, method string, args, result thrift.TStruct) error {
	if p.tracer != nil {
		headerProto, ok := p.oprot.(*thrift.THeaderProtocol)
		if ok {
			var clientSpan opentracing.Span
			if parentSpan := opentracing.SpanFromContext(ctx); parentSpan != nil {
				clientSpan = p.tracer.StartSpan(
					method,
					opentracing.ChildOf(parentSpan.Context()),
				)
			} else {
				clientSpan = p.tracer.StartSpan(method)
			}
			defer clientSpan.Finish()
			ext.SpanKindRPCClient.Set(clientSpan)
			if err := p.tracer.Inject(clientSpan.Context(), opentracing.TextMap, THeaderReaderWriter{headerProto}); err != nil {
				p.logger.Log("err", err)
			}
		}
	}
	return p.Client.Call(ctx, method, args, result)
}
