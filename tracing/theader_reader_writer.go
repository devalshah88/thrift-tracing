package tracing

import (
	"fmt"

	"github.com/devalshah88/thrift/lib/go/thrift"
)

// THeaderReaderWriter is a type that conforms to opentracing.TextMapReader and
// opentracing.TextMapWriter
// Inspired from go-kit's implementation of metadataReaderWriter
// for grpc - https://github.com/go-kit/kit/blob/master/tracing/opentracing/grpc.go
type THeaderReaderWriter struct {
	*thrift.THeaderProtocol
}

// NOTE: This works for transferring headers between go servics, but will
// not work with baseplate services since those use different trace identifier keys
// TODO: Add a hack to make this work with baseplate

// Set writes the key-val to the THeader
func (w THeaderReaderWriter) Set(key, val string) {
	w.SetHeader(key, val)
}

// ForeachKey calls the handler func for each key-value pair in THeader
func (w THeaderReaderWriter) ForeachKey(handler func(key, val string) error) error {
	fmt.Println("ForeachKey")
	for k, v := range w.ReadHeaders() {
		fmt.Println(k + " : " + v)
		if err := handler(k, v); err != nil {
			return err
		}
	}
	return nil
}
