package otlp

import (
	"context"
	"net/http"

	"github.com/Azure/adx-mon/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func LogsHandler(ctx context.Context, endpoints []string, insecureSkipVerify bool) http.HandlerFunc {

	// TODO: Why is endpoints an array? For now just select the first entry

	var opts []grpc.DialOption
	if insecureSkipVerify {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	_, err := grpc.DialContext(ctx, endpoints[0], opts...)
	if err != nil {
		logger.Fatal("Failed to dial: %v", err)
	}

	/*
		- Read the request body
		- Marshal into an OTLP protobuf using the OTLP protobuf library
		- Send the OTLP protobuf to the OTLP endpoint

		- Download the OTLP protobuf library
		- Generate Go code from the protocol buffers
		- Import the generated Go code
		- Use the generated Go code to marshal the request body into an OTLP protobuf
		- Use the generated Go code to send the OTLP protobuf to the OTLP endpoint
		- Marshal the OTLP response to bytes
		- Write the bytes to the response body
		- Send the response
	*/

	return func(w http.ResponseWriter, r *http.Request) {

		switch r.Header.Get("Content-Type") {
		case "application/x-protobuf":
			logger.Debug("Received Protobuf")
			// We're receiving an OTLP protobuf, so we just need to
		// read the body, marshal into an OTLP protobuf, then
		// use gRPC to send the OTLP protobuf to the OTLP endpoint

		case "application/json":
			logger.Debug("Received JSON")
			// We're receiving JSON, so we need to unmarshal the JSON
			// into an OTLP protobuf, then use gRPC to send the OTLP
			// protobuf to the OTLP endpoint

		default:
			logger.Debug("Unsupported Content-Type: %s", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}
