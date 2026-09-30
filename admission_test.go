// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestPrepareAdmissionLogsCapturesOnceAcrossResourcesAndScopes(t *testing.T) {
	logs := admissionCoverageLogs()
	before := snapshotLogsForTest(t, logs)
	logs.MarkReadOnly()

	admissionTime := time.Date(2026, 5, 7, 12, 13, 14, 150_000_000, time.UTC)
	var clockCalls int
	prepared := prepareAdmissionLogs(logs, func() time.Time {
		clockCalls++
		return admissionTime
	})

	require.Equal(t, 1, clockCalls)
	require.False(t, logs == prepared, "untimed records require a private deep copy")
	require.Equal(t, before, snapshotLogsForTest(t, logs))
	require.Equal(t, logs.LogRecordCount(), prepared.LogRecordCount())
	wantAdmissionTimestamp := pcommon.NewTimestampFromTime(admissionTime)
	want := map[string]struct {
		timestamp         pcommon.Timestamp
		observedTimestamp pcommon.Timestamp
	}{
		"r1.s1.untimed-a": {observedTimestamp: wantAdmissionTimestamp},
		"r1.s1.event":     {timestamp: pcommon.NewTimestampFromTime(time.Date(2026, 5, 1, 10, 0, 1, 0, time.UTC))},
		"r1.s1.observed":  {observedTimestamp: pcommon.NewTimestampFromTime(time.Date(2026, 5, 2, 10, 0, 2, 0, time.UTC))},
		"r1.s2.both": {
			timestamp:         pcommon.NewTimestampFromTime(time.Date(2026, 5, 3, 10, 0, 3, 0, time.UTC)),
			observedTimestamp: pcommon.NewTimestampFromTime(time.Date(2026, 5, 4, 10, 0, 4, 0, time.UTC)),
		},
		"r1.s2.untimed-b": {observedTimestamp: wantAdmissionTimestamp},
		"r2.s1.untimed-c": {observedTimestamp: wantAdmissionTimestamp},
	}
	for resourceIndex := 0; resourceIndex < prepared.ResourceLogs().Len(); resourceIndex++ {
		resourceLogs := prepared.ResourceLogs().At(resourceIndex)
		for scopeIndex := 0; scopeIndex < resourceLogs.ScopeLogs().Len(); scopeIndex++ {
			logRecords := resourceLogs.ScopeLogs().At(scopeIndex).LogRecords()
			for recordIndex := 0; recordIndex < logRecords.Len(); recordIndex++ {
				logRecord := logRecords.At(recordIndex)
				name := logRecord.Body().AsString()
				expected, ok := want[name]
				require.Truef(t, ok, "unexpected record %q", name)
				require.Equal(t, expected.timestamp, logRecord.Timestamp(), name)
				require.Equal(t, expected.observedTimestamp, logRecord.ObservedTimestamp(), name)
				messageKey, ok := logRecord.Attributes().Get("servicenow.message_key")
				require.True(t, ok)
				require.Equal(t, "key."+name, messageKey.AsString())
				delete(want, name)
			}
		}
	}
	require.Empty(t, want)
}

func TestPrepareAdmissionLogsSkipsClockForEmptyAndTimestampedLogs(t *testing.T) {
	t.Run("all records timestamped", func(t *testing.T) {
		logs := admissionCoverageLogs()
		fallback := pcommon.NewTimestampFromTime(time.Date(2026, 5, 8, 9, 10, 11, 0, time.UTC))
		for resourceIndex := 0; resourceIndex < logs.ResourceLogs().Len(); resourceIndex++ {
			resourceLogs := logs.ResourceLogs().At(resourceIndex)
			for scopeIndex := 0; scopeIndex < resourceLogs.ScopeLogs().Len(); scopeIndex++ {
				logRecords := resourceLogs.ScopeLogs().At(scopeIndex).LogRecords()
				for recordIndex := 0; recordIndex < logRecords.Len(); recordIndex++ {
					logRecord := logRecords.At(recordIndex)
					if logRecord.Timestamp() == 0 && logRecord.ObservedTimestamp() == 0 {
						logRecord.SetObservedTimestamp(fallback)
					}
				}
			}
		}
		before := snapshotLogsForTest(t, logs)
		clockCalls := 0
		prepared := prepareAdmissionLogs(logs, func() time.Time {
			clockCalls++
			return time.Now()
		})
		require.Zero(t, clockCalls)
		require.True(t, logs == prepared, "all-timestamped data should pass through without a copy")
		require.Equal(t, before, snapshotLogsForTest(t, prepared))
	})

	t.Run("empty", func(t *testing.T) {
		logs := plog.NewLogs()
		clockCalls := 0
		prepared := prepareAdmissionLogs(logs, func() time.Time {
			clockCalls++
			return time.Now()
		})
		require.Zero(t, clockCalls)
		require.Zero(t, prepared.LogRecordCount())
		require.True(t, logs == prepared, "empty data should pass through without a copy")
	})
}

func TestAdmissionExporterDelegatesLifecycleCapabilitiesContextAndErrors(t *testing.T) {
	for _, mutatesData := range []bool{false, true} {
		t.Run(map[bool]string{false: "read-only", true: "mutating"}[mutatesData], func(t *testing.T) {
			startErr := errors.New("start failure")
			shutdownErr := errors.New("shutdown failure")
			consumeErr := errors.New("consume failure")
			delegate := &admissionLogsDelegateStub{
				capabilities: consumer.Capabilities{MutatesData: mutatesData},
				startErr:     startErr,
				shutdownErr:  shutdownErr,
				consumeErr:   consumeErr,
				observed:     make(chan admissionObservation, 1),
			}
			admissionTime := time.Date(2026, 5, 9, 8, 7, 6, 0, time.UTC)
			wrapped := newAdmissionLogsExporter(delegate, func() time.Time { return admissionTime })
			require.Equal(t, delegate.capabilities, wrapped.Capabilities())

			startCtx := context.WithValue(context.Background(), admissionTestContextKey(1), "start")
			require.ErrorIs(t, wrapped.Start(startCtx, nil), startErr)
			require.Same(t, startCtx, delegate.startContext)

			shutdownCtx := context.WithValue(context.Background(), admissionTestContextKey(2), "shutdown")
			require.ErrorIs(t, wrapped.Shutdown(shutdownCtx), shutdownErr)
			require.Same(t, shutdownCtx, delegate.shutdownContext)

			consumeCtx := context.WithValue(context.Background(), admissionTestContextKey(3), "consume")
			logs := queueTestLogs("delegated")
			require.ErrorIs(t, wrapped.ConsumeLogs(consumeCtx, logs), consumeErr)
			observation := <-delegate.observed
			require.Same(t, consumeCtx, observation.ctx)
			require.Equal(t, "delegated", observation.body)
			require.Equal(t, admissionTime, observation.observedTimestamp.AsTime())
		})
	}
}

func TestAdmissionExporterSamplesEachConcurrentCallIndependently(t *testing.T) {
	baseTime := time.Date(2026, 5, 10, 11, 12, 13, 0, time.UTC)
	var clockCalls atomic.Int32
	delegate := &admissionLogsDelegateStub{
		observed: make(chan admissionObservation, 2),
	}
	wrapped := newAdmissionLogsExporter(delegate, func() time.Time {
		call := clockCalls.Add(1)
		return baseTime.Add(time.Duration(call) * time.Second)
	})
	logs := queueTestLogs("concurrent")
	logs.MarkReadOnly()
	before := snapshotLogsForTest(t, logs)

	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errors <- wrapped.ConsumeLogs(context.Background(), logs)
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, int32(2), clockCalls.Load())
	require.Equal(t, before, snapshotLogsForTest(t, logs))

	got := make([]string, 0, 2)
	for range 2 {
		observation := <-delegate.observed
		got = append(got, observation.observedTimestamp.AsTime().UTC().Format(serviceNowTimeLayout))
	}
	require.ElementsMatch(t, []string{
		baseTime.Add(time.Second).Format(serviceNowTimeLayout),
		baseTime.Add(2 * time.Second).Format(serviceNowTimeLayout),
	}, got)
}

func TestAdmissionTimestampSurvivesPdataProtobufRoundTripAndLegacyFallback(t *testing.T) {
	logs := queueTestLogs("protobuf")
	admissionTime := time.Date(2026, 5, 11, 12, 13, 14, 0, time.UTC)
	prepared := prepareAdmissionLogs(logs, func() time.Time { return admissionTime })
	encoded, err := (&plog.ProtoMarshaler{}).MarshalLogs(prepared)
	require.NoError(t, err)
	decoded, err := (&plog.ProtoUnmarshaler{}).UnmarshalLogs(encoded)
	require.NoError(t, err)

	cfg := createDefaultConfig().(*Config)
	laterTime := time.Date(2026, 5, 12, 1, 2, 3, 0, time.UTC)
	mapperClockCalls := 0
	records, err := mapLogsToRecordsWithClock(decoded, cfg, func() time.Time {
		mapperClockCalls++
		return laterTime
	})
	require.NoError(t, err)
	require.Zero(t, mapperClockCalls)
	require.Len(t, records, 1)
	require.Equal(t, "2026-05-11 12:13:14", records[0].TimeOfEvent)

	legacyRecords, err := mapLogsToRecordsWithClock(queueTestLogs("legacy"), cfg, func() time.Time {
		return laterTime
	})
	require.NoError(t, err)
	require.Len(t, legacyRecords, 1)
	require.Equal(t, "2026-05-12 01:02:03", legacyRecords[0].TimeOfEvent)
}

func TestFactoryQueueDelayUsesAdmissionTime(t *testing.T) {
	var clockCalls atomic.Int32
	baseTime := time.Date(2026, 5, 13, 14, 15, 16, 0, time.UTC)
	firstRequestStarted := make(chan struct{}, 1)
	releaseFirstRequest := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(releaseFirstRequest) }) }
	defer releaseFirst()
	received := make(chan eventPayload, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload eventPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		if len(payload.Records) == 1 && payload.Records[0].MetricName == "retry.blocker" {
			firstRequestStarted <- struct{}{}
			<-releaseFirstRequest
		}
		received <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer func() {
		releaseFirst()
		server.Close()
	}()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = configoptional.Some(exporterhelper.QueueBatchConfig{
		Sizer:        exporterhelper.RequestSizerTypeRequests,
		QueueSize:    10,
		NumConsumers: 1,
		Batch:        configoptional.None[exporterhelper.BatchConfig](),
	})
	cfg.BackOffConfig.Enabled = false
	logsExporter, err := createLogsExporterWithClock(context.Background(), exportertest.NewNopSettings(Type), cfg, func() time.Time {
		call := clockCalls.Add(1)
		return baseTime.Add(time.Duration(call) * time.Second)
	})
	require.NoError(t, err)
	require.False(t, logsExporter.Capabilities().MutatesData)
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	t.Cleanup(func() { require.NoError(t, logsExporter.Shutdown(context.Background())) })

	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), queueTestLogs("blocker")))
	select {
	case <-firstRequestStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the queue consumer to enter the blocking request")
	}
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), queueTestLogs("delayed")))
	releaseFirst()

	seenDelayed := false
	for !seenDelayed {
		select {
		case payload := <-received:
			for _, record := range payload.Records {
				if record.MetricName == "retry.delayed" {
					require.Equal(t, baseTime.Add(2*time.Second).Format(serviceNowTimeLayout), record.TimeOfEvent)
					seenDelayed = true
				}
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for the queued target request")
		}
	}
	require.Equal(t, int32(2), clockCalls.Load())
}

func TestFactoryQueueBatchMergeAndSplitPreservesAdmissionTimes(t *testing.T) {
	baseTime := time.Date(2026, 5, 14, 15, 16, 17, 0, time.UTC)
	var clockCalls atomic.Int32
	received := make(chan eventPayload, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload eventPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		received <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := createDefaultConfig().(*Config)
	cfg.ClientConfig.Endpoint = server.URL
	cfg.Mode = ModeMID
	cfg.QueueSettings = queueBatchingSettings(2, 3, 300*time.Millisecond)
	cfg.BackOffConfig.Enabled = false
	logsExporter, err := createLogsExporterWithClock(context.Background(), exportertest.NewNopSettings(Type), cfg, func() time.Time {
		call := clockCalls.Add(1)
		return baseTime.Add(time.Duration(call) * time.Second)
	})
	require.NoError(t, err)
	require.True(t, logsExporter.Capabilities().MutatesData, "helper batching advertises its effective capability")
	require.NoError(t, logsExporter.Start(context.Background(), hostWithExtensions{}))
	t.Cleanup(func() { require.NoError(t, logsExporter.Shutdown(context.Background())) })

	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), queueTestLogs("admission-a")))
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), queueTestLogs("admission-b1", "admission-b2", "admission-b3")))
	require.NoError(t, logsExporter.ConsumeLogs(context.Background(), queueTestLogs("admission-c")))

	seen := make(map[string]string, 5)
	var requestSizes []int
	deadline := time.After(4 * time.Second)
	for len(seen) < 5 {
		select {
		case payload := <-received:
			requestSizes = append(requestSizes, len(payload.Records))
			for _, record := range payload.Records {
				seen[record.MetricName] = record.TimeOfEvent
			}
		case <-deadline:
			t.Fatalf("timed out waiting for batched records; got=%v", seen)
		}
	}
	require.ElementsMatch(t, []int{2, 3}, requestSizes)
	require.Equal(t, map[string]string{
		"retry.admission-a":  baseTime.Add(time.Second).Format(serviceNowTimeLayout),
		"retry.admission-b1": baseTime.Add(2 * time.Second).Format(serviceNowTimeLayout),
		"retry.admission-b2": baseTime.Add(2 * time.Second).Format(serviceNowTimeLayout),
		"retry.admission-b3": baseTime.Add(2 * time.Second).Format(serviceNowTimeLayout),
		"retry.admission-c":  baseTime.Add(3 * time.Second).Format(serviceNowTimeLayout),
	}, seen)
	require.Equal(t, int32(3), clockCalls.Load())
}

func admissionCoverageLogs() plog.Logs {
	logs := plog.NewLogs()
	firstResource := logs.ResourceLogs().AppendEmpty()
	firstResource.Resource().Attributes().PutStr("service.name", "service-a")
	firstResource.Resource().Attributes().PutStr("host.name", "host-a")
	firstScope := firstResource.ScopeLogs().AppendEmpty().LogRecords()
	appendAdmissionTimestampRecord(firstScope, "r1.s1.untimed-a", "key.r1.s1.untimed-a", time.Time{}, time.Time{})
	appendAdmissionTimestampRecord(firstScope, "r1.s1.event", "key.r1.s1.event", time.Date(2026, 5, 1, 10, 0, 1, 0, time.UTC), time.Time{})
	appendAdmissionTimestampRecord(firstScope, "r1.s1.observed", "key.r1.s1.observed", time.Time{}, time.Date(2026, 5, 2, 10, 0, 2, 0, time.UTC))
	secondScope := firstResource.ScopeLogs().AppendEmpty().LogRecords()
	appendAdmissionTimestampRecord(secondScope, "r1.s2.both", "key.r1.s2.both", time.Date(2026, 5, 3, 10, 0, 3, 0, time.UTC), time.Date(2026, 5, 4, 10, 0, 4, 0, time.UTC))
	appendAdmissionTimestampRecord(secondScope, "r1.s2.untimed-b", "key.r1.s2.untimed-b", time.Time{}, time.Time{})

	secondResource := logs.ResourceLogs().AppendEmpty()
	secondResource.Resource().Attributes().PutStr("service.name", "service-b")
	secondResource.Resource().Attributes().PutStr("host.name", "host-b")
	thirdScope := secondResource.ScopeLogs().AppendEmpty().LogRecords()
	appendAdmissionTimestampRecord(thirdScope, "r2.s1.untimed-c", "key.r2.s1.untimed-c", time.Time{}, time.Time{})
	return logs
}

func queueTestLogs(names ...string) plog.Logs {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	resourceLogs.Resource().Attributes().PutStr("service.name", "admission-test")
	resourceLogs.Resource().Attributes().PutStr("host.name", "host-admission")
	logRecords := resourceLogs.ScopeLogs().AppendEmpty().LogRecords()
	for _, name := range names {
		appendAdmissionTimestampRecord(logRecords, name, "key."+name, time.Time{}, time.Time{})
	}
	return logs
}

type admissionTestContextKey int

type admissionObservation struct {
	ctx               context.Context
	body              string
	observedTimestamp pcommon.Timestamp
}

type admissionLogsDelegateStub struct {
	capabilities    consumer.Capabilities
	startErr        error
	shutdownErr     error
	consumeErr      error
	startContext    context.Context
	shutdownContext context.Context
	observed        chan admissionObservation
}

func (s *admissionLogsDelegateStub) Start(ctx context.Context, _ component.Host) error {
	s.startContext = ctx
	return s.startErr
}

func (s *admissionLogsDelegateStub) Shutdown(ctx context.Context) error {
	s.shutdownContext = ctx
	return s.shutdownErr
}

func (s *admissionLogsDelegateStub) Capabilities() consumer.Capabilities {
	return s.capabilities
}

func (s *admissionLogsDelegateStub) ConsumeLogs(ctx context.Context, logs plog.Logs) error {
	for resourceIndex := 0; resourceIndex < logs.ResourceLogs().Len(); resourceIndex++ {
		resourceLogs := logs.ResourceLogs().At(resourceIndex)
		for scopeIndex := 0; scopeIndex < resourceLogs.ScopeLogs().Len(); scopeIndex++ {
			logRecords := resourceLogs.ScopeLogs().At(scopeIndex).LogRecords()
			for recordIndex := 0; recordIndex < logRecords.Len(); recordIndex++ {
				logRecord := logRecords.At(recordIndex)
				s.observed <- admissionObservation{
					ctx:               ctx,
					body:              logRecord.Body().AsString(),
					observedTimestamp: logRecord.ObservedTimestamp(),
				}
			}
		}
	}
	return s.consumeErr
}

var _ exporter.Logs = (*admissionLogsDelegateStub)(nil)
