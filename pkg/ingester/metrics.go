package ingester

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/pao214/loki/pkg/validation"
)

type ingesterMetrics struct {
	checkpointDeleteFail       prometheus.Counter
	checkpointDeleteTotal      prometheus.Counter
	checkpointCreationFail     prometheus.Counter
	checkpointCreationTotal    prometheus.Counter
	checkpointDuration         prometheus.Summary
	checkpointLoggedBytesTotal prometheus.Counter

	walDiskFullFailures     prometheus.Counter
	walReplayActive         prometheus.Gauge
	walReplayDuration       prometheus.Gauge
	walReplaySamplesDropped *prometheus.CounterVec
	walReplayBytesDropped   *prometheus.CounterVec
	walCorruptionsTotal     *prometheus.CounterVec
	walLoggedBytesTotal     prometheus.Counter
	walRecordsLogged        prometheus.Counter

	recoveredStreamsTotal prometheus.Counter
	recoveredChunksTotal  prometheus.Counter
	recoveredEntriesTotal prometheus.Counter
	duplicateEntriesTotal prometheus.Counter
	recoveredBytesTotal   prometheus.Counter
	recoveryBytesInUse    prometheus.Gauge
	recoveryIsFlushing    prometheus.Gauge

	limiterEnabled prometheus.Gauge

	autoForgetUnhealthyIngestersTotal prometheus.Counter
}

// setRecoveryBytesInUse bounds the bytes reports to >= 0.
// TODO(owen-d): we can gain some efficiency by having the flusher never update this after recovery ends.
func (m *ingesterMetrics) setRecoveryBytesInUse(v int64) {
	if v < 0 {
		v = 0
	}
	m.recoveryBytesInUse.Set(float64(v))
}

const (
	walTypeCheckpoint = "checkpoint"
	walTypeSegment    = "segment"

	duplicateReason = "duplicate"
)

func newIngesterMetrics(r prometheus.Registerer) *ingesterMetrics {
	return &ingesterMetrics{
		walDiskFullFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_disk_full_failures_total",
			Help: "Total number of wal write failures due to full disk.",
		}),
		walReplayActive: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_wal_replay_active",
			Help: "Whether the WAL is replaying",
		}),
		walReplayDuration: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_wal_replay_duration_seconds",
			Help: "Time taken to replay the checkpoint and the WAL.",
		}),
		walReplaySamplesDropped: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_ingester_wal_discarded_samples_total",
			Help: "WAL segment entries discarded during replay",
		}, []string{validation.ReasonLabel}),
		walReplayBytesDropped: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_ingester_wal_discarded_bytes_total",
			Help: "WAL segment bytes discarded during replay",
		}, []string{validation.ReasonLabel}),
		walCorruptionsTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_ingester_wal_corruptions_total",
			Help: "Total number of WAL corruptions encountered.",
		}, []string{"type"}),
		checkpointDeleteFail: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_checkpoint_deletions_failed_total",
			Help: "Total number of checkpoint deletions that failed.",
		}),
		checkpointDeleteTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_checkpoint_deletions_total",
			Help: "Total number of checkpoint deletions attempted.",
		}),
		checkpointCreationFail: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_checkpoint_creations_failed_total",
			Help: "Total number of checkpoint creations that failed.",
		}),
		checkpointCreationTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_checkpoint_creations_total",
			Help: "Total number of checkpoint creations attempted.",
		}),
		checkpointDuration: promauto.With(r).NewSummary(prometheus.SummaryOpts{
			Name:       "loki_ingester_checkpoint_duration_seconds",
			Help:       "Time taken to create a checkpoint.",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		}),
		walRecordsLogged: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_records_logged_total",
			Help: "Total number of WAL records logged.",
		}),
		checkpointLoggedBytesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_checkpoint_logged_bytes_total",
			Help: "Total number of bytes written to disk for checkpointing.",
		}),
		walLoggedBytesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_logged_bytes_total",
			Help: "Total number of bytes written to disk for WAL records.",
		}),
		recoveredStreamsTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_recovered_streams_total",
			Help: "Total number of streams recovered from the WAL.",
		}),
		recoveredChunksTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_recovered_chunks_total",
			Help: "Total number of chunks recovered from the WAL checkpoints.",
		}),
		recoveredEntriesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_recovered_entries_total",
			Help: "Total number of entries recovered from the WAL.",
		}),
		duplicateEntriesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_duplicate_entries_total",
			Help: "Entries discarded during WAL replay due to existing in checkpoints.",
		}),
		recoveredBytesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_recovered_bytes_total",
			Help: "Total number of bytes recovered from the WAL.",
		}),
		recoveryBytesInUse: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_wal_bytes_in_use",
			Help: "Total number of bytes in use by the WAL recovery process.",
		}),
		recoveryIsFlushing: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_wal_replay_flushing",
			Help: "Whether the wal replay is in a flushing phase due to backpressure",
		}),
		limiterEnabled: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_limiter_enabled",
			Help: "Whether the ingester's limiter is enabled",
		}),
		autoForgetUnhealthyIngestersTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_autoforget_unhealthy_ingesters_total",
			Help: "Total number of ingesters automatically forgotten",
		}),
	}
}
