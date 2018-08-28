# thrift-tracing

This repo contains two components : 

1. The [tracing package](src/tracing) : This package provides thrift client and server wrappers to enable distributed tracing. It uses [a fork of Apache Thrift that supports THeader for Go](https://github.com/devalshah88/thrift), and builds on that. 
2. The [examples/addsvc package](src/examples/addsvc) : Contains an example client-server code that uses the wrappers from the tracing package. This code is a stripped down version of [go-kit's addsvc example](https://github.com/go-kit/kit/tree/master/examples/addsvc) (with only the thrift transport supported), with the main difference being that tracing works with the thrift code. 
