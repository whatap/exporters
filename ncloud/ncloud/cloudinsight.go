package ncloud

import (
	"fmt"
	"os"
)

const (
	defaultCWEndpoint = "https://cw.apigw.ntruss.com"
	cwBasePath        = "/cw_fea/real/cw/api"

	MultiQueryDataLimit = 20
)

// CloudInsightClient calls NCloud Cloud Insight APIs.
type CloudInsightClient struct {
	client   *Client
	endpoint string
}

// NewCloudInsightClient creates a new Cloud Insight API client.
func NewCloudInsightClient(client *Client) *CloudInsightClient {
	endpoint := defaultCWEndpoint
	if v := os.Getenv("NCLOUD_CW_API_GW"); v != "" {
		endpoint = v
	}
	return &CloudInsightClient{
		client:   client,
		endpoint: endpoint,
	}
}

func (ci *CloudInsightClient) url(path string) string {
	return ci.endpoint + cwBasePath + path
}

// --- Request / Response types ---

// SchemaKeyItem represents a product key entry from /schema/system/list.
type SchemaKeyItem struct {
	CWKey    string `json:"cw_key"`
	ProdName string `json:"prodName"`
}

// SearchMetricListRequest is the request body for metric search.
type SearchMetricListRequest struct {
	ProdKey                 string                    `json:"prodKey"`
	Query                   string                    `json:"query"`
	DimValues               []DimValue                `json:"dimValues,omitempty"`
	DimensionsSelectedList  []DimensionsSelectedEntry `json:"dimensionsSelectedList,omitempty"`
}

type DimValue struct {
	Dim   string `json:"dim"`
	Value string `json:"value"`
}

type DimensionsSelectedEntry struct {
	DimensionKey string `json:"dimensionKey"`
	DimensionValues []string `json:"dimensionValues"`
}

// SearchMetricListResponse is the response from metric search.
type SearchMetricListResponse struct {
	ProdKey string   `json:"prodKey"`
	Metrics []Metric `json:"metrics"`
}

// Metric describes an available metric in Cloud Insight.
type Metric struct {
	Desc         string              `json:"desc"`
	Dimensions   []Dimension         `json:"dimensions"`
	IDDimension  string              `json:"idDimension"`
	MetricName   string              `json:"metric"`
	Options      MetricOptions       `json:"options"`
	ProdKey      string              `json:"prodKey"`
	Unit         string              `json:"unit"`
	ProdName     string              `json:"prodName"`
}

type Dimension struct {
	Dim  string `json:"dim"`
	Desc string `json:"desc"`
}

// MetricOptions defines available aggregations per interval.
type MetricOptions struct {
	Min1  []string `json:"Min1"`
	Min5  []string `json:"Min5"`
	Min30 []string `json:"Min30"`
	Hour2 []string `json:"Hour2"`
	Day1  []string `json:"Day1"`
}

// GetAggregationsForInterval returns available aggregations for the given interval.
func (o MetricOptions) GetAggregationsForInterval(interval string) []string {
	switch interval {
	case "Min1":
		return o.Min1
	case "Min5":
		return o.Min5
	case "Min30":
		return o.Min30
	case "Hour2":
		return o.Hour2
	case "Day1":
		return o.Day1
	}
	return nil
}

// QueryDataRequest is the request for single metric data query.
type QueryDataRequest struct {
	TimeStart   int64             `json:"timeStart"`
	TimeEnd     int64             `json:"timeEnd"`
	CWKey       string            `json:"cw_key"`
	MetricName  string            `json:"metric"`
	Interval    string            `json:"interval"`
	Aggregation string            `json:"aggregation"`
	Dimensions  map[string]string `json:"dimensions"`
	ProductName string            `json:"productName"`
}

// QueryDataMultiRequest is the request for batch metric data query.
type QueryDataMultiRequest struct {
	MetricInfoList []MetricInfo `json:"metricInfoList"`
	TimeStart      int64        `json:"timeStart"`
	TimeEnd        int64        `json:"timeEnd"`
}

// MetricInfo describes one metric in a batch query.
type MetricInfo struct {
	Aggregation string            `json:"aggregation"`
	Dimensions  map[string]string `json:"dimensions"`
	Interval    string            `json:"interval"`
	MetricName  string            `json:"metric"`
	ProdKey     string            `json:"prodKey"`
}

// MetricListResponseItem is one item in the batch query response.
type MetricListResponseItem struct {
	Dimensions  map[string]string `json:"dimensions"`
	Dps         [][]interface{}   `json:"dps"`
	Aggregation string            `json:"aggregation"`
	Interval    string            `json:"interval"`
	MetricName  string            `json:"metric"`
	ProductName string            `json:"productName"`
}

// --- API methods ---

// GetSchemaKeyList returns the list of product keys (cw_key) from Cloud Insight.
// GET /schema/system/list
func (ci *CloudInsightClient) GetSchemaKeyList() ([]SchemaKeyItem, error) {
	var result []SchemaKeyItem
	err := ci.client.DoGet(ci.url("/schema/system/list"), &result)
	if err != nil {
		return nil, fmt.Errorf("get schema key list: %w", err)
	}
	return result, nil
}

// SearchMetricList searches available metrics for a given product key.
// POST /rule/group/metric/search
func (ci *CloudInsightClient) SearchMetricList(req *SearchMetricListRequest) (*SearchMetricListResponse, error) {
	var result SearchMetricListResponse
	err := ci.client.DoPost(ci.url("/rule/group/metric/search"), req, &result)
	if err != nil {
		return nil, fmt.Errorf("search metric list: %w", err)
	}
	return &result, nil
}

// QueryData queries a single metric's data points.
// POST /data/query
func (ci *CloudInsightClient) QueryData(req *QueryDataRequest) ([][]interface{}, error) {
	var result [][]interface{}
	err := ci.client.DoPost(ci.url("/data/query"), req, &result)
	if err != nil {
		return nil, fmt.Errorf("query data: %w", err)
	}
	return result, nil
}

// QueryDataMulti queries multiple metrics in a single batch request (max 20).
// POST /data/query/multiple
func (ci *CloudInsightClient) QueryDataMulti(req *QueryDataMultiRequest) ([]MetricListResponseItem, error) {
	var result []MetricListResponseItem
	err := ci.client.DoPost(ci.url("/data/query/multiple"), req, &result)
	if err != nil {
		return nil, fmt.Errorf("query data multi: %w", err)
	}
	return result, nil
}
