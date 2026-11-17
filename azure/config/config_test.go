package config

import (
	"reflect"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v2"
)

const targetHead = `
credentials:
  subscription_id: sub
  tenant_id: tenant
  client_id: client
  client_secret: secret
targets:
  - resource: /resourceGroups/rg/providers/Microsoft.EventHub/namespaces/demo
    metrics:
`

func parseAndValidate(t *testing.T, doc string) (*Config, error) {
	t.Helper()
	c := &Config{}
	if err := yaml.Unmarshal([]byte(doc), c); err != nil {
		return nil, err
	}
	return c, c.Validate()
}

func TestValidateAcceptsCountAggregation(t *testing.T) {
	_, err := parseAndValidate(t, targetHead+`      - name: IncomingMessages
        aggregations: [Total, Count]
`)
	if err != nil {
		t.Fatalf("Count must be a valid aggregation: %v", err)
	}
}

func TestValidateRejectsUnknownAggregation(t *testing.T) {
	_, err := parseAndValidate(t, targetHead+`      - name: IncomingMessages
        aggregations: [Median]
`)
	if err == nil || !strings.Contains(err.Error(), "Median") {
		t.Fatalf("want an error naming Median, got %v", err)
	}
}

func TestMetricParsesDimensionsAndTop(t *testing.T) {
	c, err := parseAndValidate(t, targetHead+`      - name: IncomingMessages
        aggregations: [Total]
        dimensions: [EntityName, OperationResult]
        top: 50
`)
	if err != nil {
		t.Fatalf("dimensions and top must be accepted: %v", err)
	}
	m := c.Targets[0].Metrics[0]
	if !reflect.DeepEqual(m.Dimensions, []string{"EntityName", "OperationResult"}) || m.Top != 50 {
		t.Fatalf("got dimensions %v top %d", m.Dimensions, m.Top)
	}
}

func TestValidateRejectsBadDimensionSettings(t *testing.T) {
	cases := map[string]string{
		"quote breaks the filter":      `        dimensions: ["Entity'Name"]`,
		"empty name":                   `        dimensions: [""]`,
		"same label after lowercasing": `        dimensions: [EntityName, entityname]`,
		"top needs dimensions":         `        top: 5`,
		"top cannot be negative":       "        dimensions: [EntityName]\n        top: -1",
	}
	for name, setting := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseAndValidate(t, targetHead+"      - name: IncomingMessages\n"+setting+"\n")
			if err == nil {
				t.Fatalf("want a validation error")
			}
		})
	}
}

func TestValidateChecksDimensionsInResourceGroupsAndTags(t *testing.T) {
	docs := map[string]string{
		"resource_groups": `
resource_groups:
  - resource_group: rg
    resource_types: ["Microsoft.EventHub/namespaces"]
    metrics:
      - name: IncomingMessages
        dimensions: ["bad'name"]
`,
		"resource_tags": `
resource_tags:
  - resource_tag_name: team
    resource_tag_value: ops
    resource_types: ["Microsoft.EventHub/namespaces"]
    metrics:
      - name: IncomingMessages
        dimensions: ["bad'name"]
`,
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAndValidate(t, doc); err == nil {
				t.Fatalf("want a validation error")
			}
		})
	}
}
