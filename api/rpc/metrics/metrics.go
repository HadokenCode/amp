package metrics

import (
	"context"
	"errors"
	"time"

	"github.com/appcelerator/amp/pkg/prometheus"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

// Metrics is used to implement metrics.MetricsServer
type Metrics struct {
	Prometheus *prometheus.Prometheus
}

// MetricsQuery extracts CPU metrics according to CPUMetricsRequest
func (m *Metrics) MetricsQuery(ctx context.Context, in *MetricsRequest) (*CPUMetricsResponse, error) {
	response := &CPUMetricsResponse{}
	log.Infoln("Get metrics:", in.String())
	if in.Cpu {
		query := "container_cpu_user_seconds_total"
		if in.Average {
			query = "avg_over_time(container_cpu_user_seconds_total[" + in.TimeRange + "m])"
		}
		log.Infoln(query)
		samples, err := m.Prometheus.Api().Query(context.Background(), query, time.Now())
		if err != nil {
			return nil, errors.New("unable to query Prometheus")
		}
		for _, sample := range samples.(model.Vector) {
			entry := &CPUMetricsEntry{
				Service: string(sample.Metric["container_label_com_docker_swarm_service_name"]),
				Usage:   float32(sample.Value / 100),
			}
			response.Entries = append(response.Entries, entry)
		}
	}
	return response, nil
}
