package ingest

import (
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

func newTracesDecoder(c Consumer) signalDecoder {
	return signalDecoder{
		decode: func(body []byte, proto bool) error {
			req := ptraceotlp.NewExportRequest()
			var err error
			if proto {
				err = req.UnmarshalProto(body)
			} else {
				err = req.UnmarshalJSON(body)
			}
			if err != nil {
				return err
			}
			c.ConsumeTraces(req.Traces())
			return nil
		},
		respond: func(proto bool) ([]byte, error) {
			resp := ptraceotlp.NewExportResponse()
			if proto {
				return resp.MarshalProto()
			}
			return resp.MarshalJSON()
		},
	}
}

func newMetricsDecoder(c Consumer) signalDecoder {
	return signalDecoder{
		decode: func(body []byte, proto bool) error {
			req := pmetricotlp.NewExportRequest()
			var err error
			if proto {
				err = req.UnmarshalProto(body)
			} else {
				err = req.UnmarshalJSON(body)
			}
			if err != nil {
				return err
			}
			c.ConsumeMetrics(req.Metrics())
			return nil
		},
		respond: func(proto bool) ([]byte, error) {
			resp := pmetricotlp.NewExportResponse()
			if proto {
				return resp.MarshalProto()
			}
			return resp.MarshalJSON()
		},
	}
}

func newLogsDecoder(c Consumer) signalDecoder {
	return signalDecoder{
		decode: func(body []byte, proto bool) error {
			req := plogotlp.NewExportRequest()
			var err error
			if proto {
				err = req.UnmarshalProto(body)
			} else {
				err = req.UnmarshalJSON(body)
			}
			if err != nil {
				return err
			}
			c.ConsumeLogs(req.Logs())
			return nil
		},
		respond: func(proto bool) ([]byte, error) {
			resp := plogotlp.NewExportResponse()
			if proto {
				return resp.MarshalProto()
			}
			return resp.MarshalJSON()
		},
	}
}
