package collector

import (
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/whatap/ncloud_exporter/config"
	"github.com/whatap/ncloud_exporter/ncloud"
)

const (
	cacheTTL    = 5 * time.Minute
	queryWindow = 10 * time.Minute
)

// NCloudCollector implements prometheus.Collector for NCloud Cloud Insight metrics.
type NCloudCollector struct {
	ci       *ncloud.CloudInsightClient
	rc       *ncloud.ResourceClient
	cfg      *config.Config
	cwKeys   map[string]string // prodName -> cw_key

	mu               sync.Mutex
	instanceCache    map[string]cachedInstances
	metricDefCache   map[string]cachedMetricDefs

	scrapeErrors *prometheus.Desc
	scrapeDuration *prometheus.Desc
}

type cachedInstances struct {
	instances []ncloud.Instance
	fetchedAt time.Time
}

type cachedMetricDefs struct {
	metrics []ncloud.Metric
	fetchedAt time.Time
}

// NewNCloudCollector creates a new collector.
func NewNCloudCollector(ci *ncloud.CloudInsightClient, rc *ncloud.ResourceClient, cfg *config.Config) *NCloudCollector {
	return &NCloudCollector{
		ci:             ci,
		rc:             rc,
		cfg:            cfg,
		cwKeys:         make(map[string]string),
		instanceCache:  make(map[string]cachedInstances),
		metricDefCache: make(map[string]cachedMetricDefs),
		scrapeErrors: prometheus.NewDesc(
			"ncloud_scrape_errors_total",
			"Number of errors during NCloud scrape",
			[]string{"namespace"}, nil,
		),
		scrapeDuration: prometheus.NewDesc(
			"ncloud_scrape_duration_seconds",
			"Duration of NCloud scrape in seconds",
			nil, nil,
		),
	}
}

// InitCWKeys fetches and caches the product key (cw_key) mapping.
func (c *NCloudCollector) InitCWKeys() error {
	items, err := c.ci.GetSchemaKeyList()
	if err != nil {
		return err
	}
	for _, item := range items {
		c.cwKeys[item.ProdName] = item.CWKey
	}
	return nil
}

func (c *NCloudCollector) Describe(ch chan<- *prometheus.Desc) {
	// Send no descriptors to mark this as an unchecked collector.
	// NCloud Cloud Insight metrics are discovered dynamically at scrape time,
	// so we cannot enumerate all possible Desc objects upfront.
}

func (c *NCloudCollector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	var totalErrors float64

	for _, ns := range c.cfg.Namespaces {
		if !ns.Enabled {
			continue
		}

		errs := c.collectNamespace(ch, ns)
		totalErrors += float64(errs)

		ch <- prometheus.MustNewConstMetric(
			c.scrapeErrors, prometheus.GaugeValue, float64(errs), ns.Name,
		)
	}

	ch <- prometheus.MustNewConstMetric(
		c.scrapeDuration, prometheus.GaugeValue, time.Since(start).Seconds(),
	)

	_ = totalErrors
}

func (c *NCloudCollector) collectNamespace(ch chan<- prometheus.Metric, ns config.NamespaceConfig) int {
	errors := 0

	svc, ok := ncloud.KnownServices[ns.Name]
	if !ok {
		slog.Warn("unknown namespace", "namespace", ns.Name)
		return 1
	}

	cwKey := c.findCWKey(svc)
	if cwKey == "" {
		slog.Warn("no cw_key found", "namespace", ns.Name)
		return 1
	}

	// Get instances (with cache)
	instances, err := c.getInstances(svc, ns)
	if err != nil {
		slog.Error("failed to get instances", "namespace", ns.Name, "error", err)
		return 1
	}

	if len(instances) == 0 {
		return 0
	}

	// Get metric definitions (with cache)
	allMetricDefs, err := c.getMetricDefs(cwKey)
	if err != nil {
		slog.Error("failed to get metric defs", "namespace", ns.Name, "error", err)
		return 1
	}

	if len(allMetricDefs) == 0 {
		return 0
	}

	interval := ns.ResolveInterval()

	// Apply the metrics filter from config
	selected := c.selectMetrics(ns, interval, allMetricDefs)
	if len(selected) == 0 {
		slog.Warn("no metrics selected", "namespace", ns.Name, "available", len(allMetricDefs))
		return 0
	}

	// Build batch queries and execute
	now := time.Now()
	timeEnd := now.UnixMilli()
	timeStart := now.Add(-queryWindow).UnixMilli()

	var batch []ncloud.MetricInfo
	type batchMeta struct {
		metricDef   ncloud.Metric
		instance    ncloud.Instance
		aggregation string
	}
	var metas []batchMeta

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}
		req := &ncloud.QueryDataMultiRequest{
			MetricInfoList: batch,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
		}

		results, err := c.ci.QueryDataMulti(req)
		if err != nil {
			slog.Error("failed to query metrics", "namespace", ns.Name, "error", err)
			errors++
			batch = nil
			metas = nil
			return
		}

		for i, result := range results {
			if i >= len(metas) {
				break
			}
			meta := metas[i]

			val, ok := ExtractLatestDatapoint(result.Dps)
			if !ok {
				continue
			}

			fqName := BuildMetricFQName(ns.Name, meta.metricDef.MetricName, meta.aggregation)

			// Build labels with deterministic key ordering
			type labelPair struct {
				key   string
				value string
			}
			pairs := make([]labelPair, 0, len(meta.instance.Tags))
			for k, v := range meta.instance.Tags {
				pairs = append(pairs, labelPair{SanitizeLabelName(k), v})
			}
			sort.Slice(pairs, func(i, j int) bool {
				return pairs[i].key < pairs[j].key
			})
			labelKeys := make([]string, len(pairs))
			labelVals := make([]string, len(pairs))
			for idx, p := range pairs {
				labelKeys[idx] = p.key
				labelVals[idx] = p.value
			}

			// Build HELP text in English
			helpText := "NCloud " + ns.Name + " " + meta.metricDef.MetricName
			if meta.metricDef.Unit != "" {
				helpText += " (" + meta.metricDef.Unit + ")"
			}

			desc := prometheus.NewDesc(fqName, helpText, labelKeys, nil)
			m, err := prometheus.NewConstMetric(desc, prometheus.GaugeValue, val, labelVals...)
			if err != nil {
				slog.Error("failed to create metric", "metric", fqName, "error", err)
				errors++
				continue
			}
			ch <- m
		}

		batch = nil
		metas = nil
	}

	for _, sel := range selected {
		for _, inst := range instances {
			dimValue := inst.InstanceNo
			if dimValue == "" {
				continue
			}

			for _, aggr := range sel.aggregations {
				mi := ncloud.MetricInfo{
					Aggregation: aggr,
					Dimensions:  map[string]string{sel.def.IDDimension: dimValue},
					Interval:    interval,
					MetricName:  sel.def.MetricName,
					ProdKey:     cwKey,
				}
				batch = append(batch, mi)
				metas = append(metas, batchMeta{
					metricDef:   sel.def,
					instance:    inst,
					aggregation: aggr,
				})

				if len(batch) >= ncloud.MultiQueryDataLimit {
					flushBatch()
				}
			}
		}
	}

	flushBatch()
	return errors
}

// selectedMetric pairs a Cloud Insight metric definition with the aggregations
// that should actually be queried for it.
type selectedMetric struct {
	def          ncloud.Metric
	aggregations []string
}

// selectMetrics applies the namespace's metrics filter to the metric definitions
// discovered from the API, and resolves the aggregation list for each survivor.
//
// Resolution order for aggregations: metrics[].aggregations -> namespace
// aggregations -> whatever the API reports as available for the interval.
// Configured aggregations that the API does not support for the interval are
// dropped with a warning, so a typo cannot silently produce empty series.
func (c *NCloudCollector) selectMetrics(ns config.NamespaceConfig, interval string, defs []ncloud.Metric) []selectedMetric {
	selected := make([]selectedMetric, 0, len(defs))
	matchedEntries := make(map[int]bool, len(ns.Metrics))

	for _, def := range defs {
		wanted, entryIdx, ok := ns.SelectMetric(def.MetricName)
		if !ok {
			continue
		}
		if entryIdx >= 0 {
			matchedEntries[entryIdx] = true
		}

		available := def.Options.GetAggregationsForInterval(interval)
		if len(available) == 0 {
			slog.Warn("metric has no aggregations for interval",
				"namespace", ns.Name, "metric", def.MetricName, "interval", interval)
			continue
		}

		aggrs := available
		if len(wanted) > 0 {
			aggrs = intersectAggregations(wanted, available)
			if len(aggrs) == 0 {
				slog.Warn("none of the configured aggregations are supported",
					"namespace", ns.Name, "metric", def.MetricName,
					"interval", interval, "configured", wanted, "available", available)
				continue
			}
			if len(aggrs) != len(wanted) {
				slog.Warn("some configured aggregations are not supported",
					"namespace", ns.Name, "metric", def.MetricName,
					"interval", interval, "using", aggrs, "available", available)
			}
		}

		selected = append(selected, selectedMetric{def: def, aggregations: aggrs})
	}

	// Surface metric names in the config that matched nothing (typos, or a
	// metric that does not exist for this product).
	for i, m := range ns.Metrics {
		if !matchedEntries[i] {
			slog.Warn("configured metric matched no available metric",
				"namespace", ns.Name, "metric", m.Name)
		}
	}

	return selected
}

// intersectAggregations keeps the wanted aggregations that the API supports,
// preserving the order given in the config. Comparison is case-insensitive.
func intersectAggregations(wanted, available []string) []string {
	supported := make(map[string]string, len(available))
	for _, a := range available {
		supported[strings.ToUpper(a)] = a
	}

	out := make([]string, 0, len(wanted))
	for _, w := range wanted {
		if actual, ok := supported[strings.ToUpper(w)]; ok {
			out = append(out, actual)
		}
	}
	return out
}

func (c *NCloudCollector) findCWKey(svc ncloud.ServiceDef) string {
	// Match by Cloud Insight prodName (e.g. "Server(VPC)")
	if svc.ProdName != "" {
		if key, ok := c.cwKeys[svc.ProdName]; ok {
			return key
		}
	}
	// Fallback: try namespace as key
	if key, ok := c.cwKeys[svc.Namespace]; ok {
		return key
	}
	return ""
}

func (c *NCloudCollector) getInstances(svc ncloud.ServiceDef, ns config.NamespaceConfig) ([]ncloud.Instance, error) {
	c.mu.Lock()
	cached, ok := c.instanceCache[ns.Name]
	c.mu.Unlock()

	if ok && time.Since(cached.fetchedAt) < cacheTTL {
		return cached.instances, nil
	}

	regions := ns.Regions
	if len(regions) == 0 {
		regList, err := c.rc.GetRegions(svc)
		if err != nil {
			return nil, err
		}
		for _, r := range regList {
			regions = append(regions, r.RegionCode)
		}
		if len(regions) == 0 {
			regions = []string{""}
		}
	}

	var allInstances []ncloud.Instance
	for _, region := range regions {
		instances, err := c.rc.ListInstances(svc, region)
		if err != nil {
			slog.Error("failed to list instances", "namespace", ns.Name, "region", region, "error", err)
			continue
		}
		allInstances = append(allInstances, instances...)
	}

	c.mu.Lock()
	c.instanceCache[ns.Name] = cachedInstances{
		instances: allInstances,
		fetchedAt: time.Now(),
	}
	c.mu.Unlock()

	return allInstances, nil
}

// ReloadConfig swaps the collector's config and clients, and clears all caches
// so that the next scrape picks up the new settings.
func (c *NCloudCollector) ReloadConfig(cfg *config.Config, ci *ncloud.CloudInsightClient, rc *ncloud.ResourceClient) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = cfg
	c.ci = ci
	c.rc = rc
	c.instanceCache = make(map[string]cachedInstances)
	c.metricDefCache = make(map[string]cachedMetricDefs)
}

func (c *NCloudCollector) getMetricDefs(cwKey string) ([]ncloud.Metric, error) {
	c.mu.Lock()
	cached, ok := c.metricDefCache[cwKey]
	c.mu.Unlock()

	if ok && time.Since(cached.fetchedAt) < cacheTTL {
		return cached.metrics, nil
	}

	resp, err := c.ci.SearchMetricList(&ncloud.SearchMetricListRequest{
		ProdKey: cwKey,
		Query:   "",
	})
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.metricDefCache[cwKey] = cachedMetricDefs{
		metrics:   resp.Metrics,
		fetchedAt: time.Now(),
	}
	c.mu.Unlock()

	return resp.Metrics, nil
}
