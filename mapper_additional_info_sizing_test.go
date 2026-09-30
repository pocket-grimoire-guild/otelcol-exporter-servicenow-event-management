// Copyright 2026 Pocket Grimoire Guild contributors
// SPDX-License-Identifier: Apache-2.0

package servicenoweventmanagementexporter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type additionalInfoSizingFixture struct {
	name     string
	required []additionalInfoEntry
	optional []additionalInfoEntry
	maxBytes int
	want     string
	wantDrop bool
	wantErr  bool
}

func TestFitAdditionalInfoSizingBoundaries(t *testing.T) {
	for _, fixture := range additionalInfoSizingFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			output, byteDropped, err := fitAdditionalInfo(fixture.required, fixture.optional, fixture.maxBytes)
			require.Equal(t, fixture.want, output)
			require.Equal(t, fixture.wantDrop, byteDropped)
			if fixture.wantErr {
				require.ErrorIs(t, err, errAdditionalInfoRequiredMetadataTooLarge)
				return
			}
			require.NoError(t, err)
		})
	}
}

func additionalInfoSizingFixtures() []additionalInfoSizingFixture {
	const droppedKey = additionalInfoDroppedAttributesKey
	const reducedKey = additionalInfoMetadataReducedKey

	largeValue := strings.Repeat("x", 120)
	exact := marshalAdditionalInfoForTest(map[string]string{"candidate": largeValue, "required": "stable"})
	requiredAndCounter := marshalAdditionalInfoForTest(map[string]string{droppedKey: "1", "required": "stable"})
	reservedMarker := additionalInfoEntry{key: reducedKey, value: "producer-forgery"}
	reservedCandidates := func(count int) []additionalInfoEntry {
		entries := make([]additionalInfoEntry, count)
		for i := range entries {
			entries[i] = reservedMarker
		}
		return entries
	}
	counterLimit := func(count int) int {
		return len(marshalAdditionalInfoForTest(map[string]string{droppedKey: strconv.Itoa(count), "tail": "t"}))
	}
	counterFixture := func(name string, markerCount, maxCount int, wantCount int, wantTail, wantByteDrop bool) additionalInfoSizingFixture {
		optional := append([]additionalInfoEntry{{key: "tail", value: "t"}}, reservedCandidates(markerCount)...)
		wantValues := map[string]string{droppedKey: strconv.Itoa(wantCount)}
		if wantTail {
			wantValues["tail"] = "t"
		}
		return additionalInfoSizingFixture{
			name:     name,
			required: []additionalInfoEntry{{key: droppedKey, value: "0"}},
			optional: optional,
			maxBytes: counterLimit(maxCount),
			want:     marshalAdditionalInfoForTest(wantValues),
			wantDrop: wantByteDrop,
		}
	}
	optionalCounterFixture := func(name string, markerCount int, want string, wantDrop, wantErr bool) additionalInfoSizingFixture {
		optional := append([]additionalInfoEntry{{key: droppedKey, value: "0"}}, reservedCandidates(markerCount)...)
		return additionalInfoSizingFixture{
			name:     name,
			optional: optional,
			maxBytes: len(marshalAdditionalInfoForTest(map[string]string{droppedKey: "9"})),
			want:     want,
			wantDrop: wantDrop,
			wantErr:  wantErr,
		}
	}

	escapedKey := "quote\" slash\\ newline\n tab\t control\x01 \u2028 \u2029 é " + string([]byte{0xff})
	escapedValue := "<>& quote\" slash\\ newline\n tab\t control\x02 \u2028 \u2029 雪 " + string([]byte{0xfe})
	escapedWant := marshalAdditionalInfoForTest(map[string]string{escapedKey: escapedValue, "required": "stable"})
	invalidReplacementKey := string([]byte{0xff})
	otherInvalidReplacementKey := string([]byte{0xfe})
	invalidKeyWant := marshalAdditionalInfoForTest(map[string]string{
		invalidReplacementKey:      "invalid-byte-ff",
		otherInvalidReplacementKey: "invalid-byte-fe",
	})
	emptyRequiredCandidate := marshalAdditionalInfoForTest(map[string]string{"candidate": "value"})

	return []additionalInfoSizingFixture{
		{
			name:     "empty_optional",
			required: []additionalInfoEntry{{key: "required", value: "stable"}},
			maxBytes: 128,
			want:     marshalAdditionalInfoForTest(map[string]string{"required": "stable"}),
		},
		{
			name:     "one_candidate",
			required: []additionalInfoEntry{{key: "required", value: "stable"}},
			optional: []additionalInfoEntry{{key: "candidate", value: "value"}},
			maxBytes: 128,
			want:     marshalAdditionalInfoForTest(map[string]string{"candidate": "value", "required": "stable"}),
		},
		{
			name:     "empty_required_map_admits_candidate_without_comma",
			optional: []additionalInfoEntry{{key: "candidate", value: "value"}},
			maxBytes: len(emptyRequiredCandidate),
			want:     emptyRequiredCandidate,
		},
		{
			name:     "collision_only_candidates",
			required: []additionalInfoEntry{{key: "duplicate", value: "required"}},
			optional: []additionalInfoEntry{
				{key: "duplicate", value: "first"},
				{key: "duplicate", value: "second"},
			},
			maxBytes: 128,
			want:     marshalAdditionalInfoForTest(map[string]string{"duplicate": "required", droppedKey: "2"}),
		},
		{
			name: "duplicate_required_and_optional_keys",
			required: []additionalInfoEntry{
				{key: "duplicate", value: "first"},
				{key: "duplicate", value: "last"},
			},
			optional: []additionalInfoEntry{
				{key: "duplicate", value: "optional"},
				{key: "other", value: "first"},
				{key: "other", value: "second"},
			},
			maxBytes: 128,
			want:     marshalAdditionalInfoForTest(map[string]string{"duplicate": "last", "other": "first", droppedKey: "2"}),
		},
		{
			name:     "oversized_candidate_then_fitting_candidate",
			required: []additionalInfoEntry{{key: "r", value: "v"}},
			optional: []additionalInfoEntry{
				{key: "large", value: strings.Repeat("z", 180)},
				{key: "small", value: "ok"},
			},
			maxBytes: 128,
			want:     marshalAdditionalInfoForTest(map[string]string{"r": "v", "small": "ok", droppedKey: "1"}),
			wantDrop: true,
		},
		{
			name:     "exact_fit",
			required: []additionalInfoEntry{{key: "required", value: "stable"}},
			optional: []additionalInfoEntry{{key: "candidate", value: largeValue}},
			maxBytes: len(exact),
			want:     exact,
		},
		{
			name:     "one_byte_below_fit",
			required: []additionalInfoEntry{{key: "required", value: "stable"}},
			optional: []additionalInfoEntry{{key: "candidate", value: largeValue}},
			maxBytes: len(exact) - 1,
			want:     requiredAndCounter,
			wantDrop: true,
		},
		{
			name:     "one_byte_above_fit",
			required: []additionalInfoEntry{{key: "required", value: "stable"}},
			optional: []additionalInfoEntry{{key: "candidate", value: largeValue}},
			maxBytes: len(exact) + 1,
			want:     exact,
		},
		{
			name:     "escaped_key_and_value_tokens_exact_fit",
			required: []additionalInfoEntry{{key: "required", value: "stable"}},
			optional: []additionalInfoEntry{{key: escapedKey, value: escapedValue}},
			maxBytes: len(escapedWant),
			want:     escapedWant,
		},
		{
			name: "distinct_invalid_utf8_raw_keys_have_same_json_token",
			optional: []additionalInfoEntry{
				{key: invalidReplacementKey, value: "invalid-byte-ff"},
				{key: otherInvalidReplacementKey, value: "invalid-byte-fe"},
			},
			maxBytes: len(invalidKeyWant),
			want:     invalidKeyWant,
		},
		{
			name:     "reserved_reduced_marker",
			required: []additionalInfoEntry{{key: "required", value: "stable"}},
			optional: []additionalInfoEntry{reservedMarker},
			maxBytes: 128,
			want:     marshalAdditionalInfoForTest(map[string]string{"required": "stable", droppedKey: "1"}),
		},
		{
			name:     "large_required_producer_counter_shrinks_to_generated_count",
			required: []additionalInfoEntry{{key: droppedKey, value: strings.Repeat("p", 200)}},
			optional: []additionalInfoEntry{reservedMarker},
			maxBytes: 128,
			want:     marshalAdditionalInfoForTest(map[string]string{droppedKey: "1"}),
		},
		{
			name:     "oversized_required_producer_counter_allows_generated_count_after_candidate_drop",
			required: []additionalInfoEntry{{key: droppedKey, value: strings.Repeat("p", 200)}},
			optional: []additionalInfoEntry{{key: "candidate", value: "small"}},
			maxBytes: 128,
			want:     marshalAdditionalInfoForTest(map[string]string{droppedKey: "1"}),
			wantDrop: true,
		},
		counterFixture("counter_growth_9_to_10_start", 9, 9, 9, true, false),
		counterFixture("counter_growth_9_to_10_after", 10, 9, 11, false, true),
		counterFixture("counter_growth_99_to_100_start", 99, 99, 99, true, false),
		counterFixture("counter_growth_99_to_100_after", 100, 99, 101, false, true),
		optionalCounterFixture("optional_producer_counter_overwritten", 9, marshalAdditionalInfoForTest(map[string]string{droppedKey: "9"}), false, false),
		optionalCounterFixture("optional_producer_counter_eviction_reinsertion_overflow", 10, "", true, true),
		{
			name:     "required_metadata_overflow",
			required: []additionalInfoEntry{{key: "required", value: strings.Repeat("r", 80)}},
			maxBytes: 8,
			wantErr:  true,
		},
	}
}

func marshalAdditionalInfoForTest(values map[string]string) string {
	encoded, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

var (
	additionalInfoFitBenchmarkSink string
	additionalInfoRecordSink       eventRecord
)

func BenchmarkFitAdditionalInfo(b *testing.B) {
	for _, size := range []int{0, 1, 16, 128, 256} {
		b.Run(fmt.Sprintf("entries_%d", size), func(b *testing.B) {
			required := []additionalInfoEntry{{key: "otel.signal", value: "logs"}}
			optional := make([]additionalInfoEntry, size)
			for i := range optional {
				optional[i] = additionalInfoEntry{
					key:   fmt.Sprintf("candidate.%03d", i),
					value: "value-0123456789",
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				encoded, _, err := fitAdditionalInfo(required, optional, 4000)
				if err != nil {
					b.Fatal(err)
				}
				additionalInfoFitBenchmarkSink = encoded
			}
		})
	}
}

func BenchmarkMapLogsToRecordsAdditionalInfo(b *testing.B) {
	for _, fixture := range []struct {
		name          string
		addedAttrs    int
		maxAttributes int
	}{
		{name: "default_cap_128_added", addedAttrs: 128, maxAttributes: defaultAdditionalInfoMaxAttributes},
		{name: "uncapped_256_added", addedAttrs: 256, maxAttributes: 0},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			cfg := createDefaultConfig().(*Config)
			cfg.AdditionalInfo.MaxAttributes = fixture.maxAttributes
			logs := sampleLogs()
			attrs := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes()
			for i := 0; i < fixture.addedAttrs; i++ {
				attrs.PutStr(fmt.Sprintf("f12.bench.%03d", i), "value-0123456789")
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				records, err := mapLogsToRecords(logs, cfg)
				if err != nil {
					b.Fatal(err)
				}
				additionalInfoRecordSink = records[0]
			}
		})
	}
}
