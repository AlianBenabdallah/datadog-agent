// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package translator

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"

	"github.com/DataDog/datadog-agent/pkg/otlp/model/source"
	"github.com/DataDog/datadog-agent/pkg/quantile"
	"github.com/DataDog/datadog-agent/pkg/quantile/summary"
	"github.com/DataDog/datadog-agent/pkg/trace/pb"
)

func TestIsCumulativeMonotonic(t *testing.T) {
	// Some of these examples are from the hostmetrics receiver
	// and reflect the semantic meaning of the metrics there.
	//
	// If the receiver changes these examples should be added here too

	{ // Sum: Cumulative but not monotonic
		metric := pmetric.NewMetric()
		metric.SetName("system.filesystem.usage")
		metric.SetDescription("Filesystem bytes used.")
		metric.SetUnit("bytes")
		metric.SetEmptySum()
		sum := metric.Sum()
		sum.SetIsMonotonic(false)
		sum.SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)

		assert.False(t, isCumulativeMonotonic(metric))
	}

	{ // Sum: Cumulative and monotonic
		metric := pmetric.NewMetric()
		metric.SetName("system.network.packets")
		metric.SetDescription("The number of packets transferred.")
		metric.SetUnit("1")
		metric.SetEmptySum()
		sum := metric.Sum()
		sum.SetIsMonotonic(true)
		sum.SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)

		assert.True(t, isCumulativeMonotonic(metric))
	}

	{ // DoubleSumL Cumulative and monotonic
		metric := pmetric.NewMetric()
		metric.SetName("metric.example")
		metric.SetEmptySum()
		sum := metric.Sum()
		sum.SetIsMonotonic(true)
		sum.SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)

		assert.True(t, isCumulativeMonotonic(metric))
	}

	{ // Not IntSum
		metric := pmetric.NewMetric()
		metric.SetName("system.cpu.load_average.1m")
		metric.SetDescription("Average CPU Load over 1 minute.")
		metric.SetUnit("1")
		metric.SetEmptyGauge()

		assert.False(t, isCumulativeMonotonic(metric))
	}
}

var _ source.Provider = (*testProvider)(nil)

type testProvider string

func (t testProvider) Source(context.Context) (source.Source, error) {
	return source.Source{
		Kind:       source.HostnameKind,
		Identifier: string(t),
	}, nil
}

func newTranslator(t *testing.T, logger *zap.Logger, opts ...Option) *Translator {
	options := append([]Option{
		WithFallbackSourceProvider(testProvider(fallbackHostname)),
		WithHistogramMode(HistogramModeDistributions),
		WithNumberMode(NumberModeCumulativeToDelta),
	}, opts...)

	tr, err := New(
		logger,
		options...,
	)

	require.NoError(t, err)
	return tr
}

type metric struct {
	name      string
	typ       MetricDataType
	timestamp uint64
	value     float64
	tags      []string
	host      string
}

type sketch struct {
	name      string
	basic     summary.Summary
	timestamp uint64
	tags      []string
	host      string
}

var _ TimeSeriesConsumer = (*mockTimeSeriesConsumer)(nil)

type mockTimeSeriesConsumer struct {
	metrics []metric
}

func (m *mockTimeSeriesConsumer) ConsumeTimeSeries(
	_ context.Context,
	dimensions *Dimensions,
	typ MetricDataType,
	ts uint64,
	val float64,
) {
	m.metrics = append(m.metrics,
		metric{
			name:      dimensions.Name(),
			typ:       typ,
			timestamp: ts,
			value:     val,
			tags:      dimensions.Tags(),
			host:      dimensions.Host(),
		},
	)
}

func newDims(name string) *Dimensions {
	return &Dimensions{name: name, tags: []string{}}
}

func newGauge(dims *Dimensions, ts uint64, val float64) metric {
	return metric{name: dims.name, typ: Gauge, timestamp: ts, value: val, tags: dims.tags}
}

func newCount(dims *Dimensions, ts uint64, val float64) metric {
	return newCountWithHost(dims, ts, val, "")
}

func newCountWithHost(dims *Dimensions, ts uint64, val float64, host string) metric {
	return metric{name: dims.name, typ: Count, timestamp: ts, value: val, tags: dims.tags, host: host}
}

func newSketch(dims *Dimensions, ts uint64, s summary.Summary) sketch {
	return sketch{name: dims.name, basic: s, timestamp: ts, tags: dims.tags}
}

func TestMapIntMetrics(t *testing.T) {
	ts := pcommon.NewTimestampFromTime(time.Now())
	slice := pmetric.NewNumberDataPointSlice()
	point := slice.AppendEmpty()
	point.SetIntValue(17)
	point.SetTimestamp(ts)
	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())

	consumer := &mockTimeSeriesConsumer{}
	dims := newDims("int64.test")
	tr.mapNumberMetrics(ctx, consumer, dims, Gauge, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{newGauge(dims, uint64(ts), 17)},
	)

	consumer = &mockTimeSeriesConsumer{}
	dims = newDims("int64.delta.test")
	tr.mapNumberMetrics(ctx, consumer, dims, Count, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{newCount(dims, uint64(ts), 17)},
	)

	// With attribute tags
	consumer = &mockTimeSeriesConsumer{}
	dims = &Dimensions{name: "int64.test", tags: []string{"attribute_tag:attribute_value"}}
	tr.mapNumberMetrics(ctx, consumer, dims, Gauge, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{newGauge(dims, uint64(ts), 17)},
	)
}

func TestMapDoubleMetrics(t *testing.T) {
	ts := pcommon.NewTimestampFromTime(time.Now())
	slice := pmetric.NewNumberDataPointSlice()
	point := slice.AppendEmpty()
	point.SetDoubleValue(math.Pi)
	point.SetTimestamp(ts)
	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())

	consumer := &mockTimeSeriesConsumer{}
	dims := newDims("float64.test")
	tr.mapNumberMetrics(ctx, consumer, dims, Gauge, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{newGauge(dims, uint64(ts), math.Pi)},
	)

	consumer = &mockTimeSeriesConsumer{}
	dims = newDims("float64.delta.test")
	tr.mapNumberMetrics(ctx, consumer, dims, Count, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{newCount(dims, uint64(ts), math.Pi)},
	)

	// With attribute tags
	consumer = &mockTimeSeriesConsumer{}
	dims = &Dimensions{name: "float64.test", tags: []string{"attribute_tag:attribute_value"}}
	tr.mapNumberMetrics(ctx, consumer, dims, Gauge, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{newGauge(dims, uint64(ts), math.Pi)},
	)
}

func seconds(i int) pcommon.Timestamp {
	return pcommon.NewTimestampFromTime(time.Unix(int64(i), 0))
}

var exampleDims = newDims("metric.example")

func TestMapIntMonotonicMetrics(t *testing.T) {
	// Create list of values
	deltas := []int64{1, 2, 200, 3, 7, 0}
	cumulative := make([]int64, len(deltas)+1)
	cumulative[0] = 0
	for i := 1; i < len(cumulative); i++ {
		cumulative[i] = cumulative[i-1] + deltas[i-1]
	}

	//Map to OpenTelemetry format
	slice := pmetric.NewNumberDataPointSlice()
	slice.EnsureCapacity(len(cumulative))
	for i, val := range cumulative {
		point := slice.AppendEmpty()
		point.SetIntValue(val)
		point.SetTimestamp(seconds(i))
	}

	// Map to Datadog format
	expected := make([]metric, len(deltas))
	for i, val := range deltas {
		expected[i] = newCount(exampleDims, uint64(seconds(i+1)), float64(val))
	}

	ctx := context.Background()
	consumer := &mockTimeSeriesConsumer{}
	tr := newTranslator(t, zap.NewNop())
	tr.mapNumberMonotonicMetrics(ctx, consumer, exampleDims, slice)

	assert.ElementsMatch(t, expected, consumer.metrics)
}

func TestMapIntMonotonicDifferentDimensions(t *testing.T) {
	slice := pmetric.NewNumberDataPointSlice()

	// No tags
	point := slice.AppendEmpty()
	point.SetTimestamp(seconds(0))

	point = slice.AppendEmpty()
	point.SetIntValue(20)
	point.SetTimestamp(seconds(1))

	// One tag: valA
	point = slice.AppendEmpty()
	point.SetTimestamp(seconds(0))
	point.Attributes().PutString("key1", "valA")

	point = slice.AppendEmpty()
	point.SetIntValue(30)
	point.SetTimestamp(seconds(1))
	point.Attributes().PutString("key1", "valA")

	// same tag: valB
	point = slice.AppendEmpty()
	point.SetTimestamp(seconds(0))
	point.Attributes().PutString("key1", "valB")

	point = slice.AppendEmpty()
	point.SetIntValue(40)
	point.SetTimestamp(seconds(1))
	point.Attributes().PutString("key1", "valB")

	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())

	consumer := &mockTimeSeriesConsumer{}
	tr.mapNumberMonotonicMetrics(ctx, consumer, exampleDims, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{
			newCount(exampleDims, uint64(seconds(1)), 20),
			newCount(exampleDims.AddTags("key1:valA"), uint64(seconds(1)), 30),
			newCount(exampleDims.AddTags("key1:valB"), uint64(seconds(1)), 40),
		},
	)
}

func TestMapIntMonotonicWithReboot(t *testing.T) {
	values := []int64{0, 30, 0, 20}
	slice := pmetric.NewNumberDataPointSlice()
	slice.EnsureCapacity(len(values))

	for i, val := range values {
		point := slice.AppendEmpty()
		point.SetTimestamp(seconds(i))
		point.SetIntValue(val)
	}

	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())
	consumer := &mockTimeSeriesConsumer{}
	tr.mapNumberMonotonicMetrics(ctx, consumer, exampleDims, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{
			newCount(exampleDims, uint64(seconds(1)), 30),
			newCount(exampleDims, uint64(seconds(3)), 20),
		},
	)
}

func TestMapIntMonotonicReportFirstValue(t *testing.T) {
	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())
	consumer := &mockFullConsumer{}
	tr.MapMetrics(ctx, createTestIntCumulativeMonotonicMetrics(), consumer)
	startTs := int(getProcessStartTime()) + 1
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{
			newCountWithHost(exampleDims, uint64(seconds(startTs+1)), 10, fallbackHostname),
			newCountWithHost(exampleDims, uint64(seconds(startTs+2)), 5, fallbackHostname),
			newCountWithHost(exampleDims, uint64(seconds(startTs+3)), 5, fallbackHostname),
		},
	)
}

func TestMapIntMonotonicReportDiffForFirstValue(t *testing.T) {
	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())
	consumer := &mockFullConsumer{}
	dims := &Dimensions{name: exampleDims.name, host: fallbackHostname}
	startTs := int(getProcessStartTime()) + 1
	// Add an entry to the cache about the timeseries, in this case we send the diff (9) rather than the first value (10).
	tr.prevPts.MonotonicDiff(dims, uint64(seconds(startTs)), uint64(seconds(startTs+1)), 1)
	tr.MapMetrics(ctx, createTestIntCumulativeMonotonicMetrics(), consumer)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{
			newCountWithHost(exampleDims, uint64(seconds(startTs+1)), 9, fallbackHostname),
			newCountWithHost(exampleDims, uint64(seconds(startTs+2)), 5, fallbackHostname),
			newCountWithHost(exampleDims, uint64(seconds(startTs+3)), 5, fallbackHostname),
		},
	)
}

func TestMapIntMonotonicOutOfOrder(t *testing.T) {
	stamps := []int{1, 0, 2, 3}
	values := []int64{0, 1, 2, 3}

	slice := pmetric.NewNumberDataPointSlice()
	slice.EnsureCapacity(len(values))

	for i, val := range values {
		point := slice.AppendEmpty()
		point.SetTimestamp(seconds(stamps[i]))
		point.SetIntValue(val)
	}

	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())
	consumer := &mockTimeSeriesConsumer{}
	tr.mapNumberMonotonicMetrics(ctx, consumer, exampleDims, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{
			newCount(exampleDims, uint64(seconds(2)), 2),
			newCount(exampleDims, uint64(seconds(3)), 1),
		},
	)
}

func TestMapDoubleMonotonicMetrics(t *testing.T) {
	deltas := []float64{1, 2, 200, 3, 7, 0}
	cumulative := make([]float64, len(deltas)+1)
	cumulative[0] = 0
	for i := 1; i < len(cumulative); i++ {
		cumulative[i] = cumulative[i-1] + deltas[i-1]
	}

	//Map to OpenTelemetry format
	slice := pmetric.NewNumberDataPointSlice()
	slice.EnsureCapacity(len(cumulative))
	for i, val := range cumulative {
		point := slice.AppendEmpty()
		point.SetDoubleValue(val)
		point.SetTimestamp(seconds(i))
	}

	// Map to Datadog format
	expected := make([]metric, len(deltas))
	for i, val := range deltas {
		expected[i] = newCount(exampleDims, uint64(seconds(i+1)), val)
	}

	ctx := context.Background()
	consumer := &mockTimeSeriesConsumer{}
	tr := newTranslator(t, zap.NewNop())
	tr.mapNumberMonotonicMetrics(ctx, consumer, exampleDims, slice)

	assert.ElementsMatch(t, expected, consumer.metrics)
}

func TestMapDoubleMonotonicDifferentDimensions(t *testing.T) {
	slice := pmetric.NewNumberDataPointSlice()

	// No tags
	point := slice.AppendEmpty()
	point.SetTimestamp(seconds(0))

	point = slice.AppendEmpty()
	point.SetDoubleValue(20)
	point.SetTimestamp(seconds(1))

	// One tag: valA
	point = slice.AppendEmpty()
	point.SetTimestamp(seconds(0))
	point.Attributes().PutString("key1", "valA")

	point = slice.AppendEmpty()
	point.SetDoubleValue(30)
	point.SetTimestamp(seconds(1))
	point.Attributes().PutString("key1", "valA")

	// one tag: valB
	point = slice.AppendEmpty()
	point.SetTimestamp(seconds(0))
	point.Attributes().PutString("key1", "valB")

	point = slice.AppendEmpty()
	point.SetDoubleValue(40)
	point.SetTimestamp(seconds(1))
	point.Attributes().PutString("key1", "valB")

	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())

	consumer := &mockTimeSeriesConsumer{}
	tr.mapNumberMonotonicMetrics(ctx, consumer, exampleDims, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{
			newCount(exampleDims, uint64(seconds(1)), 20),
			newCount(exampleDims.AddTags("key1:valA"), uint64(seconds(1)), 30),
			newCount(exampleDims.AddTags("key1:valB"), uint64(seconds(1)), 40),
		},
	)
}

func TestMapDoubleMonotonicWithReboot(t *testing.T) {
	values := []float64{0, 30, 0, 20}
	slice := pmetric.NewNumberDataPointSlice()
	slice.EnsureCapacity(len(values))

	for i, val := range values {
		point := slice.AppendEmpty()
		point.SetTimestamp(seconds(2 * i))
		point.SetDoubleValue(val)
	}

	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())
	consumer := &mockTimeSeriesConsumer{}
	tr.mapNumberMonotonicMetrics(ctx, consumer, exampleDims, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{
			newCount(exampleDims, uint64(seconds(2)), 30),
			newCount(exampleDims, uint64(seconds(6)), 20),
		},
	)
}

func TestMapDoubleMonotonicReportFirstValue(t *testing.T) {
	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())
	consumer := &mockFullConsumer{}
	tr.MapMetrics(ctx, createTestDoubleCumulativeMonotonicMetrics(), consumer)
	startTs := int(getProcessStartTime()) + 1
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{
			newCountWithHost(exampleDims, uint64(seconds(startTs+1)), 10, fallbackHostname),
			newCountWithHost(exampleDims, uint64(seconds(startTs+2)), 5, fallbackHostname),
			newCountWithHost(exampleDims, uint64(seconds(startTs+3)), 5, fallbackHostname),
		},
	)
}

func TestMapAPMStats(t *testing.T) {
	consumer := &mockFullConsumer{}
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	tr := newTranslator(t, logger)
	md := tr.StatsPayloadToMetrics(pb.StatsPayload{
		Stats: []pb.ClientStatsPayload{statsPayloads[0], statsPayloads[1]},
	})

	ctx := context.Background()
	tr.MapMetrics(ctx, md, consumer)
	require.Equal(t, consumer.apmstats, statsPayloads)
}

func TestMapDoubleMonotonicReportDiffForFirstValue(t *testing.T) {
	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())
	consumer := &mockFullConsumer{}
	dims := &Dimensions{name: exampleDims.name, host: fallbackHostname}
	startTs := int(getProcessStartTime()) + 1
	// Add an entry to the cache about the timeseries, in this case we send the diff (9) rather than the first value (10).
	tr.prevPts.MonotonicDiff(dims, uint64(seconds(startTs)), uint64(seconds(startTs+1)), 1)
	tr.MapMetrics(ctx, createTestDoubleCumulativeMonotonicMetrics(), consumer)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{
			newCountWithHost(exampleDims, uint64(seconds(startTs+1)), 9, fallbackHostname),
			newCountWithHost(exampleDims, uint64(seconds(startTs+2)), 5, fallbackHostname),
			newCountWithHost(exampleDims, uint64(seconds(startTs+3)), 5, fallbackHostname),
		},
	)
}

func TestMapDoubleMonotonicOutOfOrder(t *testing.T) {
	stamps := []int{1, 0, 2, 3}
	values := []float64{0, 1, 2, 3}

	slice := pmetric.NewNumberDataPointSlice()
	slice.EnsureCapacity(len(values))

	for i, val := range values {
		point := slice.AppendEmpty()
		point.SetTimestamp(seconds(stamps[i]))
		point.SetDoubleValue(val)
	}

	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())
	consumer := &mockTimeSeriesConsumer{}
	tr.mapNumberMonotonicMetrics(ctx, consumer, exampleDims, slice)
	assert.ElementsMatch(t,
		consumer.metrics,
		[]metric{
			newCount(exampleDims, uint64(seconds(2)), 2),
			newCount(exampleDims, uint64(seconds(3)), 1),
		},
	)
}

var _ SketchConsumer = (*mockFullConsumer)(nil)

type mockFullConsumer struct {
	mockTimeSeriesConsumer
	sketches []sketch
	apmstats []pb.ClientStatsPayload
}

func (c *mockFullConsumer) ConsumeAPMStats(p pb.ClientStatsPayload) {
	c.apmstats = append(c.apmstats, p)
}

func (c *mockFullConsumer) ConsumeSketch(_ context.Context, dimensions *Dimensions, ts uint64, sk *quantile.Sketch) {
	c.sketches = append(c.sketches,
		sketch{
			name:      dimensions.Name(),
			basic:     sk.Basic,
			timestamp: ts,
			tags:      dimensions.Tags(),
			host:      dimensions.Host(),
		},
	)
}

func TestLegacyBucketsTags(t *testing.T) {
	// Test that passing the same tags slice doesn't reuse the slice.
	ctx := context.Background()
	tr := newTranslator(t, zap.NewNop())

	tags := make([]string, 0, 10)

	pointOne := pmetric.NewHistogramDataPoint()
	pointOne.BucketCounts().FromRaw([]uint64{2, 18})
	pointOne.ExplicitBounds().FromRaw([]float64{0})
	pointOne.SetTimestamp(seconds(0))
	consumer := &mockTimeSeriesConsumer{}
	dims := &Dimensions{name: "test.histogram.one", tags: tags}
	tr.getLegacyBuckets(ctx, consumer, dims, pointOne, true)
	seriesOne := consumer.metrics

	pointTwo := pmetric.NewHistogramDataPoint()
	pointTwo.BucketCounts().FromRaw([]uint64{2, 18})
	pointTwo.ExplicitBounds().FromRaw([]float64{1})
	pointTwo.SetTimestamp(seconds(0))
	consumer = &mockTimeSeriesConsumer{}
	dims = &Dimensions{name: "test.histogram.two", tags: tags}
	tr.getLegacyBuckets(ctx, consumer, dims, pointTwo, true)
	seriesTwo := consumer.metrics

	assert.ElementsMatch(t, seriesOne[0].tags, []string{"lower_bound:-inf", "upper_bound:0"})
	assert.ElementsMatch(t, seriesTwo[0].tags, []string{"lower_bound:-inf", "upper_bound:1.0"})
}

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		f float64
		s string
	}{
		{f: 0, s: "0"},
		{f: 0.001, s: "0.001"},
		{f: 0.9, s: "0.9"},
		{f: 0.95, s: "0.95"},
		{f: 0.99, s: "0.99"},
		{f: 0.999, s: "0.999"},
		{f: 1, s: "1.0"},
		{f: 2, s: "2.0"},
		{f: math.Inf(1), s: "inf"},
		{f: math.Inf(-1), s: "-inf"},
		{f: math.NaN(), s: "nan"},
		{f: 1e-10, s: "1e-10"},
	}

	for _, test := range tests {
		assert.Equal(t, test.s, formatFloat(test.f))
	}
}

const (
	testHostname     = "res-hostname"
	fallbackHostname = "fallbackHostname"
)

func createTestIntCumulativeMonotonicMetrics() pmetric.Metrics {
	md := pmetric.NewMetrics()
	rms := md.ResourceMetrics()
	rm := rms.AppendEmpty()

	attrs := rm.Resource().Attributes()
	attrs.PutString(attributes.AttributeDatadogHostname, testHostname)
	for attr, val := range additionalAttributes {
		attrs.PutString(attr, val)
	}
	ilms := rm.ScopeMetrics()

	ilm := ilms.AppendEmpty()
	ilm.Scope().SetName(name)
	ilm.Scope().SetVersion(version)
	metricsArray := ilm.Metrics()
	metricsArray.AppendEmpty() // first one is TypeNone to test that it's ignored

	// IntGauge
	met := metricsArray.AppendEmpty()
	met.SetName("int.gauge")
	met.SetEmptyGauge()
	dpsInt := met.Gauge().DataPoints()
	dpInt := dpsInt.AppendEmpty()
	dpInt.SetTimestamp(seconds(0))
	dpInt.SetIntValue(1)

	// DoubleGauge
	met = metricsArray.AppendEmpty()
	met.SetName("double.gauge")
	met.SetEmptyGauge()
	dpsDouble := met.Gauge().DataPoints()
	dpDouble := dpsDouble.AppendEmpty()
	dpDouble.SetTimestamp(seconds(0))
	dpDouble.SetDoubleValue(math.Pi)

	// aggregation unspecified sum
	met = metricsArray.AppendEmpty()
	met.SetName("unspecified.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityUnspecified)

	// Int Sum (delta)
	met = metricsArray.AppendEmpty()
	met.SetName("int.delta.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
	dpsInt = met.Sum().DataPoints()
	dpInt = dpsInt.AppendEmpty()
	dpInt.SetTimestamp(seconds(0))
	dpInt.SetIntValue(2)

	// Double Sum (delta)
	met = metricsArray.AppendEmpty()
	met.SetName("double.delta.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
	dpsDouble = met.Sum().DataPoints()
	dpDouble = dpsDouble.AppendEmpty()
	dpDouble.SetTimestamp(seconds(0))
	dpDouble.SetDoubleValue(math.E)

	// Int Sum (delta monotonic)
	met = metricsArray.AppendEmpty()
	met.SetName("int.delta.monotonic.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
	dpsInt = met.Sum().DataPoints()
	dpInt = dpsInt.AppendEmpty()
	dpInt.SetTimestamp(seconds(0))
	dpInt.SetIntValue(2)

	// Double Sum (delta monotonic)
	met = metricsArray.AppendEmpty()
	met.SetName("double.delta.monotonic.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
	dpsDouble = met.Sum().DataPoints()
	dpDouble = dpsDouble.AppendEmpty()
	dpDouble.SetTimestamp(seconds(0))
	dpDouble.SetDoubleValue(math.E)

	// aggregation unspecified histogram
	met = metricsArray.AppendEmpty()
	met.SetName("unspecified.histogram")
	met.SetEmptyHistogram()
	met.Histogram().SetAggregationTemporality(pmetric.MetricAggregationTemporalityUnspecified)

	// Histogram (delta)
	met = metricsArray.AppendEmpty()
	met.SetName("double.histogram")
	met.SetEmptyHistogram()
	met.Histogram().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
	dpsDoubleHist := met.Histogram().DataPoints()
	dpDoubleHist := dpsDoubleHist.AppendEmpty()
	dpDoubleHist.SetCount(20)
	dpDoubleHist.SetSum(math.Phi)
	dpDoubleHist.BucketCounts().FromRaw([]uint64{2, 18})
	dpDoubleHist.ExplicitBounds().FromRaw([]float64{0})
	dpDoubleHist.SetTimestamp(seconds(0))

	// Int Sum (cumulative)
	met = metricsArray.AppendEmpty()
	met.SetName("int.cumulative.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)
	dpsInt = met.Sum().DataPoints()
	dpsInt.EnsureCapacity(2)
	dpInt = dpsInt.AppendEmpty()
	dpInt.SetTimestamp(seconds(0))
	dpInt.SetIntValue(4)

	// Double Sum (cumulative)
	met = metricsArray.AppendEmpty()
	met.SetName("double.cumulative.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)
	dpsDouble = met.Sum().DataPoints()
	dpsDouble.EnsureCapacity(2)
	dpDouble = dpsDouble.AppendEmpty()
	dpDouble.SetTimestamp(seconds(0))
	dpDouble.SetDoubleValue(4)

	// Int Sum (cumulative monotonic)
	met = metricsArray.AppendEmpty()
	met.SetName("int.cumulative.monotonic.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)
	met.Sum().SetIsMonotonic(true)

	values := []int64{10, 15, 20}
	dpsInt := met.Sum().DataPoints()
	dpsInt.EnsureCapacity(len(values))

	startTs := int(getProcessStartTime()) + 1
	for i, val := range values {
		dpInt := dpsInt.AppendEmpty()
		dpInt.SetStartTimestamp(seconds(startTs))
		dpInt.SetTimestamp(seconds(startTs + i + 1))
		dpInt.SetIntValue(val)
	}
	return md
}

func createTestDoubleCumulativeMonotonicMetrics() pmetric.Metrics {
	md := pmetric.NewMetrics()
	met := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	met.SetName(exampleDims.name)
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)
	met.Sum().SetIsMonotonic(true)

func createTestIntCumulativeMonotonicMetrics() pmetric.Metrics {
	md := pmetric.NewMetrics()
	met := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	met.SetName(exampleDims.name)
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	met.Sum().SetIsMonotonic(true)

	values := []int64{10, 15, 20}
	dpsInt := met.Sum().DataPoints()
	dpsInt.EnsureCapacity(len(values))

	startTs := int(getProcessStartTime()) + 1
	for i, val := range values {
		dpInt := dpsInt.AppendEmpty()
		dpInt.SetStartTimestamp(seconds(startTs))
		dpInt.SetTimestamp(seconds(startTs + i + 1))
		dpInt.SetIntValue(val)
	}
	return md
}

func createTestDoubleCumulativeMonotonicMetrics() pmetric.Metrics {
	md := pmetric.NewMetrics()
	met := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	met.SetName(exampleDims.name)
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	met.Sum().SetIsMonotonic(true)

	values := []float64{10, 15, 20}
	dpsInt := met.Sum().DataPoints()
	dpsInt.EnsureCapacity(len(values))

	startTs := int(getProcessStartTime()) + 1
	for i, val := range values {
		dpInt := dpsInt.AppendEmpty()
		dpInt.SetStartTimestamp(seconds(startTs))
		dpInt.SetTimestamp(seconds(startTs + i + 1))
		dpInt.SetDoubleValue(val)
	}
	return md
}

func newCountWithHostname(name string, val float64, seconds uint64, tags []string) metric {
	dims := newDims(name)
	m := newCount(dims.AddTags(tags...), seconds*1e9, val)
	m.host = testHostname
	return m
}

func newSketchWithHostname(name string, summary summary.Summary, tags []string) sketch {
	dims := newDims(name)
	s := newSketch(dims.AddTags(tags...), 0, summary)
	s.host = testHostname
	return s
}

func createNaNMetrics() pmetric.Metrics {
	md := pmetric.NewMetrics()
	rms := md.ResourceMetrics()
	rm := rms.AppendEmpty()

	attrs := rm.Resource().Attributes()
	attrs.PutString(attributes.AttributeDatadogHostname, testHostname)
	ilms := rm.ScopeMetrics()

	metricsArray := ilms.AppendEmpty().Metrics()

	// DoubleGauge
	met := metricsArray.AppendEmpty()
	met.SetName("nan.gauge")
	met.SetEmptyGauge()
	dpsDouble := met.Gauge().DataPoints()
	dpDouble := dpsDouble.AppendEmpty()
	dpDouble.SetTimestamp(seconds(0))
	dpDouble.SetDoubleValue(math.NaN())

	// Double Sum (delta)
	met = metricsArray.AppendEmpty()
	met.SetName("nan.delta.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
	dpsDouble = met.Sum().DataPoints()
	dpDouble = dpsDouble.AppendEmpty()
	dpDouble.SetTimestamp(seconds(0))
	dpDouble.SetDoubleValue(math.NaN())

	// Double Sum (delta monotonic)
	met = metricsArray.AppendEmpty()
	met.SetName("nan.delta.monotonic.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
	dpsDouble = met.Sum().DataPoints()
	dpDouble = dpsDouble.AppendEmpty()
	dpDouble.SetTimestamp(seconds(0))
	dpDouble.SetDoubleValue(math.NaN())

	// Histogram
	met = metricsArray.AppendEmpty()
	met.SetName("nan.histogram")
	met.SetEmptyHistogram()
	met.Histogram().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
	dpsDoubleHist := met.Histogram().DataPoints()
	dpDoubleHist := dpsDoubleHist.AppendEmpty()
	dpDoubleHist.SetCount(20)
	dpDoubleHist.SetSum(math.NaN())
	dpDoubleHist.BucketCounts().FromRaw([]uint64{2, 18})
	dpDoubleHist.ExplicitBounds().FromRaw([]float64{0})
	dpDoubleHist.SetTimestamp(seconds(0))

	// Double Sum (cumulative)
	met = metricsArray.AppendEmpty()
	met.SetName("nan.cumulative.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)
	dpsDouble = met.Sum().DataPoints()
	dpsDouble.EnsureCapacity(2)
	dpDouble = dpsDouble.AppendEmpty()
	dpDouble.SetTimestamp(seconds(0))
	dpDouble.SetDoubleValue(math.NaN())

	// Double Sum (cumulative monotonic)
	met = metricsArray.AppendEmpty()
	met.SetName("nan.cumulative.monotonic.sum")
	met.SetEmptySum()
	met.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)
	met.Sum().SetIsMonotonic(true)
	dpsDouble = met.Sum().DataPoints()
	dpsDouble.EnsureCapacity(2)
	dpDouble = dpsDouble.AppendEmpty()
	dpDouble.SetTimestamp(seconds(0))
	dpDouble.SetDoubleValue(math.NaN())

	// Summary
	met = metricsArray.AppendEmpty()
	met.SetName("nan.summary")
	met.SetEmptySummary()
	slice := exampleSummaryDataPointSlice(seconds(0), math.NaN(), 1)
	slice.CopyTo(met.Summary().DataPoints())

	met = metricsArray.AppendEmpty()
	met.SetName("nan.summary")
	met.SetEmptySummary()
	slice = exampleSummaryDataPointSlice(seconds(2), 10_001, 101)
	slice.CopyTo(met.Summary().DataPoints())
	return md
}

func TestNaNMetrics(t *testing.T) {
	md := createNaNMetrics()

	core, observed := observer.New(zapcore.DebugLevel)
	testLogger := zap.New(core)
	ctx := context.Background()
	tr := newTranslator(t, testLogger)
	consumer := &mockFullConsumer{}
	err := tr.MapMetrics(ctx, md, consumer)
	require.NoError(t, err)

	assert.ElementsMatch(t, consumer.metrics, []metric{
		newCountWithHostname("nan.summary.count", 100, 2, []string{}),
	})

	assert.ElementsMatch(t, consumer.sketches, []sketch{
		newSketchWithHostname("nan.histogram", summary.Summary{
			Min: 0,
			Max: 0,
			Sum: 0,
			Avg: 0,
			Cnt: 20,
		}, []string{}),
	})

	// One metric type was unknown or unsupported
	assert.Equal(t, observed.FilterMessage("Unsupported metric value").Len(), 7)
}

func TestMapExponentialHistogram(t *testing.T) {
	attrs := map[string]string{
		conventions.AttributeDeploymentEnvironment: "dev",
		"custom_attribute":                         "custom_value",
	}

	// Attributes defined in internal/attributes get converted to tags.
	// Other tags do not get converted if ResourceAttributesAsTags is false,
	// or are converted into datapoint-level attributes (which are then converted to tags) by
	// the resourcetotelemetry helper if ResourceAttributesAsTags is true
	// (outside of the MapMetrics function's scope).
	attrTags := []string{
		"env:dev",
	}

	instrumentationName := "foo"
	instrumentationVersion := "1.0.0"
	ilTags := []string{
		fmt.Sprintf("instrumentation_library:%s", instrumentationName),
		fmt.Sprintf("instrumentation_library_version:%s", instrumentationVersion),
	}
	isTags := []string{
		fmt.Sprintf("instrumentation_scope:%s", instrumentationName),
		fmt.Sprintf("instrumentation_scope_version:%s", instrumentationVersion),
	}

	tests := []struct {
		resourceAttributesAsTags                  bool
		instrumentationLibraryMetadataAsTags      bool
		instrumentationScopeMetadataAsTags        bool
		withCountSum                              bool
		expectedMetrics                           []metric
		expectedSketches                          []sketch
		expectedUnknownMetricType                 int
		expectedUnsupportedAggregationTemporality int
	}{
		{
			resourceAttributesAsTags:             false,
			instrumentationLibraryMetadataAsTags: false,
			instrumentationScopeMetadataAsTags:   false,
			withCountSum:                         false,
			expectedMetrics:                      nil,
			expectedSketches: []sketch{
				newSketchWithHostname("double.exponential.delta.histogram", summary.Summary{
					Min: -100_000,
					Max: 100_000,
					Sum: math.Pi,
					Avg: math.Pi / float64(30),
					Cnt: 30,
				}, attrTags),
			},
			expectedUnknownMetricType:                 1,
			expectedUnsupportedAggregationTemporality: 1,
		},
		{
			resourceAttributesAsTags:             true,
			instrumentationLibraryMetadataAsTags: false,
			instrumentationScopeMetadataAsTags:   false,
			withCountSum:                         false,
			expectedMetrics:                      nil,
			expectedSketches: []sketch{
				newSketchWithHostname("double.exponential.delta.histogram", summary.Summary{
					Min: -100_000,
					Max: 100_000,
					Sum: math.Pi,
					Avg: 0.10471975511965977,
					Cnt: 30,
				}, attrTags),
			},
			expectedUnknownMetricType:                 1,
			expectedUnsupportedAggregationTemporality: 1,
		},
		{
			resourceAttributesAsTags:             false,
			instrumentationLibraryMetadataAsTags: false,
			instrumentationScopeMetadataAsTags:   false,
			withCountSum:                         true,
			expectedMetrics: []metric{
				newCountWithHostname("double.exponential.delta.histogram.count", 30, 0, attrTags),
				newCountWithHostname("double.exponential.delta.histogram.sum", math.Pi, 0, attrTags),
			},
			expectedSketches: []sketch{
				newSketchWithHostname("double.exponential.delta.histogram", summary.Summary{
					Min: -100_000,
					Max: 100_000,
					Sum: math.Pi,
					Avg: 0.10471975511965977,
					Cnt: 30,
				}, attrTags),
			},
			expectedUnknownMetricType:                 1,
			expectedUnsupportedAggregationTemporality: 1,
		},
		{
			resourceAttributesAsTags:             false,
			instrumentationLibraryMetadataAsTags: true,
			instrumentationScopeMetadataAsTags:   false,
			withCountSum:                         false,
			expectedMetrics:                      nil,
			expectedSketches: []sketch{
				newSketchWithHostname("double.exponential.delta.histogram", summary.Summary{
					Min: -100_000,
					Max: 100_000,
					Sum: math.Pi,
					Avg: 0.10471975511965977,
					Cnt: 30,
				}, append(attrTags, ilTags...)),
			},
			expectedUnknownMetricType:                 1,
			expectedUnsupportedAggregationTemporality: 1,
		},
		{
			resourceAttributesAsTags:             false,
			instrumentationLibraryMetadataAsTags: false,
			instrumentationScopeMetadataAsTags:   true,
			withCountSum:                         false,
			expectedMetrics:                      nil,
			expectedSketches: []sketch{
				newSketchWithHostname("double.exponential.delta.histogram", summary.Summary{
					Min: -100_000,
					Max: 100_000,
					Sum: math.Pi,
					Avg: 0.10471975511965977,
					Cnt: 30,
				}, append(attrTags, isTags...)),
			},
			expectedUnknownMetricType:                 1,
			expectedUnsupportedAggregationTemporality: 1,
		},
		{
			resourceAttributesAsTags:             false,
			instrumentationLibraryMetadataAsTags: true,
			instrumentationScopeMetadataAsTags:   false,
			withCountSum:                         true,
			expectedMetrics: []metric{
				newCountWithHostname("double.exponential.delta.histogram.count", 30, 0, append(attrTags, ilTags...)),
				newCountWithHostname("double.exponential.delta.histogram.sum", math.Pi, 0, append(attrTags, ilTags...)),
			},
			expectedSketches: []sketch{
				newSketchWithHostname("double.exponential.delta.histogram", summary.Summary{
					Min: -100_000,
					Max: 100_000,
					Sum: math.Pi,
					Avg: 0.10471975511965977,
					Cnt: 30,
				}, append(attrTags, ilTags...)),
			},
		},
		{
			resourceAttributesAsTags:             true,
			instrumentationLibraryMetadataAsTags: true,
			instrumentationScopeMetadataAsTags:   false,
			withCountSum:                         false,
			expectedSketches: []sketch{
				newSketchWithHostname("double.exponential.delta.histogram", summary.Summary{
					Min: -100_000,
					Max: 100_000,
					Sum: math.Pi,
					Avg: 0.10471975511965977,
					Cnt: 30,
				}, append(attrTags, ilTags...)),
			},
			expectedUnknownMetricType:                 1,
			expectedUnsupportedAggregationTemporality: 1,
		},
		{
			resourceAttributesAsTags:             true,
			instrumentationLibraryMetadataAsTags: true,
			instrumentationScopeMetadataAsTags:   false,
			withCountSum:                         true,
			expectedMetrics: []metric{
				newCountWithHostname("double.exponential.delta.histogram.count", 30, 0, append(attrTags, ilTags...)),
				newCountWithHostname("double.exponential.delta.histogram.sum", math.Pi, 0, append(attrTags, ilTags...)),
			},
			expectedSketches: []sketch{
				newSketchWithHostname("double.exponential.delta.histogram", summary.Summary{
					Min: -100_000,
					Max: 100_000,
					Sum: math.Pi,
					Avg: 0.10471975511965977,
					Cnt: 30,
				}, append(attrTags, ilTags...)),
			},
		},
		{
			resourceAttributesAsTags:             true,
			instrumentationLibraryMetadataAsTags: true,
			instrumentationScopeMetadataAsTags:   true,
			withCountSum:                         true,
			expectedMetrics: []metric{
				newCountWithHostname("double.exponential.delta.histogram.count", 30, 0, append(attrTags, isTags...)),
				newCountWithHostname("double.exponential.delta.histogram.sum", math.Pi, 0, append(attrTags, isTags...)),
			},
			expectedSketches: []sketch{
				newSketchWithHostname("double.exponential.delta.histogram", summary.Summary{
					Min: -100_000,
					Max: 100_000,
					Sum: math.Pi,
					Avg: 0.10471975511965977,
					Cnt: 30,
				}, append(attrTags, isTags...)),
			},
			expectedUnknownMetricType:                 1,
			expectedUnsupportedAggregationTemporality: 1,
		},
	}

	for _, testInstance := range tests {
		tName := fmt.Sprintf("resourceAttributesAsTags: %t, instrumentationLibraryMetadataAsTags: %t, instrumentationScopeMetadataAsTags: %t, withCountSum: %t",
			testInstance.resourceAttributesAsTags, testInstance.instrumentationLibraryMetadataAsTags, testInstance.instrumentationScopeMetadataAsTags, testInstance.withCountSum)
		t.Run(tName, func(t *testing.T) {
			md := createTestExponentialHistogram(attrs, instrumentationName, instrumentationVersion)

			core, observed := observer.New(zapcore.DebugLevel)
			testLogger := zap.New(core)
			ctx := context.Background()
			consumer := &mockFullConsumer{}

			options := []Option{}
			if testInstance.resourceAttributesAsTags {
				options = append(options, WithResourceAttributesAsTags())
			}
			if testInstance.instrumentationLibraryMetadataAsTags {
				options = append(options, WithInstrumentationLibraryMetadataAsTags())
			}
			if testInstance.instrumentationScopeMetadataAsTags {
				options = append(options, WithInstrumentationScopeMetadataAsTags())
			}
			if testInstance.withCountSum {
				options = append(options, WithCountSumMetrics())
			}

			tr := newTranslator(t, testLogger, options...)
			err := tr.MapMetrics(ctx, md, consumer)
			require.NoError(t, err)

			assert.Equal(t, testInstance.expectedMetrics, consumer.metrics, "consumer.metrics doesn't have the expected one")
			assert.Equal(t, len(testInstance.expectedSketches), len(consumer.sketches), "sketches list doesn't have the expected size")
			if len(testInstance.expectedSketches) == len(consumer.sketches) {
				for i := 0; i < len(consumer.sketches); i++ {
					assert.Equal(t, testInstance.expectedSketches[i].name, consumer.sketches[i].name)
					assert.Equal(t, testInstance.expectedSketches[i].host, consumer.sketches[i].host)
					assert.Equal(t, testInstance.expectedSketches[i].tags, consumer.sketches[i].tags)
					assert.InDelta(t, testInstance.expectedSketches[i].basic.Min, consumer.sketches[i].basic.Min, 0.01)
					assert.InDelta(t, testInstance.expectedSketches[i].basic.Max, consumer.sketches[i].basic.Max, 0.01)
					assert.Equal(t, testInstance.expectedSketches[i].basic.Cnt, consumer.sketches[i].basic.Cnt)
					assert.Equal(t, testInstance.expectedSketches[i].basic.Sum, consumer.sketches[i].basic.Sum)
					assert.Equal(t, testInstance.expectedSketches[i].basic.Avg, consumer.sketches[i].basic.Avg)
				}
			}
			assert.Equal(t, testInstance.expectedUnknownMetricType, observed.FilterMessage("Unknown or unsupported metric type").Len())
			assert.Equal(t, testInstance.expectedUnsupportedAggregationTemporality, observed.FilterMessage("Unknown or unsupported aggregation temporality").Len())
		})
	}
}

func createTestExponentialHistogram(additionalAttributes map[string]string, name, version string) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rms := md.ResourceMetrics()
	rm := rms.AppendEmpty()

	attrs := rm.Resource().Attributes()
	attrs.PutString(attributes.AttributeDatadogHostname, testHostname)
	for attr, val := range additionalAttributes {
		attrs.PutString(attr, val)
	}
	ilms := rm.ScopeMetrics()

	ilm := ilms.AppendEmpty()
	ilm.Scope().SetName(name)
	ilm.Scope().SetVersion(version)
	metricsArray := ilm.Metrics()
	metricsArray.AppendEmpty() // first one is TypeNone to test that it's ignored

	// Exponential Histogram (delta)
	met := metricsArray.AppendEmpty()
	met.SetName("double.exponential.delta.histogram")
	met.SetEmptyExponentialHistogram()
	met.ExponentialHistogram().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
	expDeltaHistDp := met.ExponentialHistogram().DataPoints()
	expDeltaHist := expDeltaHistDp.AppendEmpty()
	expDeltaHist.SetScale(6)
	expDeltaHist.SetCount(30)
	expDeltaHist.SetZeroCount(10)
	expDeltaHist.SetSum(math.Pi)
	expDeltaHist.SetMin(-100_000)
	expDeltaHist.SetMax(100_000)
	expDeltaHist.Negative().SetOffset(2)

	expDeltaHist.Negative().BucketCounts().FromRaw([]uint64{3, 2, 5})
	expDeltaHist.Positive().SetOffset(3)
	expDeltaHist.Positive().BucketCounts().FromRaw([]uint64{1, 1, 1, 2, 2, 3})
	expDeltaHist.SetTimestamp(seconds(0))

	// Exponential Histogram (cumulative)
	met = metricsArray.AppendEmpty()
	met.SetName("double.exponential.cumulative.histogram")
	met.SetEmptyExponentialHistogram()
	met.ExponentialHistogram().SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)
	expCumHistDp := met.ExponentialHistogram().DataPoints()
	expCumHist := expCumHistDp.AppendEmpty()
	expCumHist.SetScale(6)
	expCumHist.SetCount(30)
	expDeltaHist.SetZeroCount(10)
	expCumHist.SetSum(math.Pi)
	expCumHist.Negative().SetOffset(2)
	expCumHist.Negative().BucketCounts().FromRaw([]uint64{3, 2, 5})
	expCumHist.Positive().SetOffset(3)
	expCumHist.Positive().BucketCounts().FromRaw([]uint64{1, 1, 1, 2, 2, 3})
	expCumHist.SetTimestamp(seconds(0))
	return md
}
