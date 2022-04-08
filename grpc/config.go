// Package grpchelpers includes helpers to set up grpc connections.
package grpc

import "fmt"

// ServiceConfig returns a JSON-encoded service config. Takes a
// service name as defined in the service's .proto file
// (e.g. "package.ServiceName").
func ServiceConfig(serviceName string) string {
	return fmt.Sprintf(serviceConfigJSON, serviceName)
}

const serviceConfigJSON = `
{
  "loadBalancingConfig": [ { "round_robin": {} } ],
  "methodConfig": [
    {
      "name": [
        {
          "service": "%s"
        }
      ],

      "retryPolicy": {
        "maxAttempts": 3,
        "initialBackoff": "0.5s",
        "maxBackoff": "2s",
        "backoffMultiplier": 1.5,
        "retryableStatusCodes": [
          "DEADLINE_EXCEEDED",
          "UNAVAILABLE"
        ]
      }
    }
  ]
}
`
