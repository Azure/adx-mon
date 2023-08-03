package otlp

import (
	"context"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	connect_go "github.com/bufbuild/connect-go"
)

type logsServer struct{}

func NewLogsServer() logsv1connect.LogsServiceHandler {
	logger.Info("Initializing OTLP logs server")
	return &logsServer{}
}

func (srv *logsServer) Export(ctx context.Context, req *connect_go.Request[v1.ExportLogsServiceRequest]) (*connect_go.Response[v1.ExportLogsServiceResponse], error) {
	// TODO: Here we're implementing a stub with the intent of receiving logs from Collector
	// for the purposes of testing. While we're not yet persisting these logs, we'll return
	// a successful response so we're able to wire up all the downstream components.
	if logger.IsDebug() && req != nil {
		logger.Debug("Received %d logs", len(req.Msg.GetResourceLogs()))
	}
	return connect_go.NewResponse(&v1.ExportLogsServiceResponse{}), nil
}
