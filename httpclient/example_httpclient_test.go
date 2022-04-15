package httpclient_test

import (
	"context"

	hc "github.com/circleci/ex/httpclient"
)

func Example_routeParams() {
	q := map[string]string{
		"branch":                 "BranchName",
		"reporting-window":       "ReportingWindow",
		"analytics-segmentation": "bxp-service",
	}
	req := hc.NewRequest("GET", "/v2/service/%s/%s/%s/workflows/%s/summary",
		hc.RouteParams("VcsType", "OrgName", "ProjectName", "WorkflowName"),
		hc.QueryParams(q),
	)

	client := hc.New(
		hc.Config{
			Name:       "my client",
			BaseURL:    "http://127.0.0.1:52484/api",
			AcceptType: hc.JSON,
		})

	err := client.Call(context.Background(), req)
	if err != nil {
		// do something
	}
}
