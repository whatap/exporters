package ncloud

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
)

const defaultAPIGWEndpoint = "https://ncloud.apigw.ntruss.com"

// Instance represents a discovered NCloud resource instance.
type Instance struct {
	InstanceNo string
	Tags       map[string]string
}

// ServiceDef describes the API endpoint for a specific NCloud service.
type ServiceDef struct {
	Namespace   string // e.g. "ncloud.vserver"
	ProdName    string // Cloud Insight prodName e.g. "Server(VPC)"
	BasePath    string // e.g. "/vserver/v2"
	ListPath    string // e.g. "/getServerInstanceList"
	RegionPath  string // e.g. "/getRegionList"
	IDField     string // JSON field containing instance ID
	NameField   string // JSON field containing instance name
	ListField   string // JSON field containing the list in the response
	ExtraTags   func(map[string]interface{}) map[string]string
}

// KnownServices maps namespace names to their service definitions.
var KnownServices = map[string]ServiceDef{
	"ncloud.vserver": {
		Namespace:  "ncloud.vserver",
		ProdName:   "Server(VPC)",
		BasePath:   "/vserver/v2",
		ListPath:   "/getServerInstanceList",
		RegionPath: "/getRegionList",
		IDField:    "serverInstanceNo",
		NameField:  "serverName",
		ListField:  "serverInstanceList",
		ExtraTags:  vserverExtraTags,
	},
	"ncloud.vloadbalancer": {
		Namespace:  "ncloud.vloadbalancer",
		ProdName:   "Load Balancer Monitor(VPC)",
		BasePath:   "/vloadbalancer/v2",
		ListPath:   "/getLoadBalancerInstanceList",
		RegionPath: "/getRegionList",
		IDField:    "loadBalancerInstanceNo",
		NameField:  "loadBalancerName",
		ListField:  "loadBalancerInstanceList",
	},
	"ncloud.vautoscaling": {
		Namespace:  "ncloud.vautoscaling",
		ProdName:   "Auto Scaling Group(VPC)",
		BasePath:   "/vautoscaling/v2",
		ListPath:   "/getAutoScalingGroupList",
		RegionPath: "/getRegionList",
		IDField:    "autoScalingGroupNo",
		NameField:  "autoScalingGroupName",
		ListField:  "autoScalingGroupList",
	},
	"ncloud.vmysql": {
		Namespace:  "ncloud.vmysql",
		ProdName:   "Cloud DB for MySQL(VPC)",
		BasePath:   "/vmysql/v2",
		ListPath:   "/getCloudMysqlInstanceList",
		RegionPath: "/getRegionList",
		IDField:    "cloudMysqlInstanceNo",
		NameField:  "cloudMysqlServiceName",
		ListField:  "cloudMysqlInstanceList",
	},
	"ncloud.vpostgresql": {
		Namespace:  "ncloud.vpostgresql",
		ProdName:   "Cloud DB for PostgreSQL(VPC)",
		BasePath:   "/vpostgresql/v2",
		ListPath:   "/getCloudPostgresqlInstanceList",
		RegionPath: "/getRegionList",
		IDField:    "cloudPostgresqlInstanceNo",
		NameField:  "cloudPostgresqlServiceName",
		ListField:  "cloudPostgresqlInstanceList",
	},
	"ncloud.vredis": {
		Namespace:  "ncloud.vredis",
		ProdName:   "Cloud DB for Cache(VPC)",
		BasePath:   "/vredis/v2",
		ListPath:   "/getCloudRedisInstanceList",
		RegionPath: "/getRegionList",
		IDField:    "cloudRedisInstanceNo",
		NameField:  "cloudRedisServiceName",
		ListField:  "cloudRedisInstanceList",
	},
	"ncloud.vmongodb": {
		Namespace:  "ncloud.vmongodb",
		ProdName:   "Cloud DB for MongoDB(VPC)",
		BasePath:   "/vmongodb/v2",
		ListPath:   "/getCloudMongoDbInstanceList",
		RegionPath: "/getRegionList",
		IDField:    "cloudMongoDbInstanceNo",
		NameField:  "cloudMongoDbServiceName",
		ListField:  "cloudMongoDbInstanceList",
	},
	"ncloud.vmssql": {
		Namespace:  "ncloud.vmssql",
		ProdName:   "Cloud DB for MSSQL(VPC)",
		BasePath:   "/vmssql/v2",
		ListPath:   "/getCloudMssqlInstanceList",
		RegionPath: "/getRegionList",
		IDField:    "cloudMssqlInstanceNo",
		NameField:  "cloudMssqlServiceName",
		ListField:  "cloudMssqlInstanceList",
	},
	"ncloud.vnks": {
		Namespace:  "ncloud.vnks",
		ProdName:   "Kubernetes Service(VPC)",
		BasePath:   "/vnks/v2",
		ListPath:   "/clusters",
		RegionPath: "",
		IDField:    "uuid",
		NameField:  "name",
		ListField:  "clusters",
	},
	"ncloud.vsearchengine": {
		Namespace:  "ncloud.vsearchengine",
		ProdName:   "Search Engine Service(VPC)",
		BasePath:   "/vsearchengine/v2",
		ListPath:   "/getClusterInfoList",
		RegionPath: "/getRegionList",
		IDField:    "clusterInstanceNo",
		NameField:  "clusterName",
		ListField:  "clusterInfoList",
	},
	"ncloud.vhadoop": {
		Namespace:  "ncloud.vhadoop",
		ProdName:   "Cloud Hadoop(VPC)",
		BasePath:   "/vhadoop/v2",
		ListPath:   "/getCloudHadoopInstanceList",
		RegionPath: "/getRegionList",
		IDField:    "cloudHadoopInstanceNo",
		NameField:  "cloudHadoopClusterName",
		ListField:  "cloudHadoopInstanceList",
	},
}

func vserverExtraTags(item map[string]interface{}) map[string]string {
	tags := make(map[string]string)
	for _, key := range []string{"privateIp", "publicIp", "serverDescription"} {
		if v, ok := item[key]; ok && v != nil {
			tags[key] = fmt.Sprintf("%v", v)
		}
	}
	if r, ok := item["region"].(map[string]interface{}); ok {
		if rc, ok := r["regionCode"]; ok {
			tags["regionCode"] = fmt.Sprintf("%v", rc)
		}
	}
	if z, ok := item["zone"].(map[string]interface{}); ok {
		if zc, ok := z["zoneCode"]; ok {
			tags["zoneCode"] = fmt.Sprintf("%v", zc)
		}
	}
	if sit, ok := item["serverInstanceType"].(map[string]interface{}); ok {
		if cn, ok := sit["codeName"]; ok {
			tags["serverInstanceType"] = fmt.Sprintf("%v", cn)
		}
	}
	return tags
}

// ResourceClient discovers NCloud resource instances.
type ResourceClient struct {
	client   *Client
	endpoint string
}

// NewResourceClient creates a new resource discovery client.
func NewResourceClient(client *Client) *ResourceClient {
	endpoint := defaultAPIGWEndpoint
	if v := os.Getenv("NCLOUD_API_GW"); v != "" {
		endpoint = v
	}
	return &ResourceClient{
		client:   client,
		endpoint: endpoint,
	}
}

// Region represents an NCloud region.
type Region struct {
	RegionNo   string `json:"regionNo"`
	RegionCode string `json:"regionCode"`
	RegionName string `json:"regionName"`
}

// fallbackRegionPath is used when a service does not have its own region list API.
const fallbackRegionBasePath = "/vserver/v2"

// GetRegions returns available regions for a service.
// If the service-specific region API returns 404, it falls back to the vserver region API.
func (rc *ResourceClient) GetRegions(svc ServiceDef) ([]Region, error) {
	if svc.RegionPath == "" {
		return nil, nil
	}

	regions, err := rc.getRegionList(svc.BasePath + svc.RegionPath)
	if err != nil {
		if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == 404 {
			slog.Warn("region API not found, falling back to vserver", "namespace", svc.Namespace)
			return rc.getRegionList(fallbackRegionBasePath + "/getRegionList")
		}
		return nil, fmt.Errorf("get regions for %s: %w", svc.Namespace, err)
	}
	return regions, nil
}

func (rc *ResourceClient) getRegionList(path string) ([]Region, error) {
	apiURL := rc.endpoint + path + "?responseFormatType=json"

	var resp struct {
		GetRegionListResponse struct {
			RegionList []Region `json:"regionList"`
		} `json:"getRegionListResponse"`
	}
	if err := rc.client.DoGet(apiURL, &resp); err != nil {
		return nil, err
	}
	return resp.GetRegionListResponse.RegionList, nil
}

// ListInstances returns instances for a given service and region.
func (rc *ResourceClient) ListInstances(svc ServiceDef, regionCode string) ([]Instance, error) {
	params := url.Values{}
	params.Set("responseFormatType", "json")
	if regionCode != "" {
		params.Set("regionCode", regionCode)
	}

	apiURL := rc.endpoint + svc.BasePath + svc.ListPath + "?" + params.Encode()

	var rawResp json.RawMessage
	if err := rc.client.DoGet(apiURL, &rawResp); err != nil {
		return nil, fmt.Errorf("list instances for %s (region=%s): %w", svc.Namespace, regionCode, err)
	}

	return parseInstanceList(rawResp, svc, regionCode)
}

func parseInstanceList(rawResp json.RawMessage, svc ServiceDef, regionCode string) ([]Instance, error) {
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(rawResp, &outer); err != nil {
		return nil, fmt.Errorf("unmarshal outer response: %w", err)
	}

	// The response wraps the list in a response key. Try common patterns.
	var listRaw json.RawMessage
	found := false

	for _, raw := range outer {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(raw, &inner); err != nil {
			// Might be the list directly (e.g. vnks /clusters)
			continue
		}
		if lr, ok := inner[svc.ListField]; ok {
			listRaw = lr
			found = true
			break
		}
	}

	// For APIs like vnks that return the list directly
	if !found {
		if lr, ok := outer[svc.ListField]; ok {
			listRaw = lr
			found = true
		}
	}

	if !found {
		slog.Warn("no list field found in response", "field", svc.ListField, "namespace", svc.Namespace)
		return nil, nil
	}

	var items []map[string]interface{}
	if err := json.Unmarshal(listRaw, &items); err != nil {
		return nil, fmt.Errorf("unmarshal instance list: %w", err)
	}

	instances := make([]Instance, 0, len(items))
	for _, item := range items {
		inst := Instance{
			Tags: map[string]string{
				"regionCode": regionCode,
			},
		}

		if v, ok := item[svc.IDField]; ok && v != nil {
			inst.InstanceNo = fmt.Sprintf("%v", v)
			inst.Tags["instanceNo"] = inst.InstanceNo
		}
		if v, ok := item[svc.NameField]; ok && v != nil {
			inst.Tags["instanceName"] = fmt.Sprintf("%v", v)
		}

		if svc.ExtraTags != nil {
			for k, v := range svc.ExtraTags(item) {
				inst.Tags[k] = v
			}
		}

		instances = append(instances, inst)
	}

	return instances, nil
}
