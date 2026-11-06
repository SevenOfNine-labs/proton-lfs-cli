//go:build integration

package integration

import "testing"

func TestParseBridgeDetailsDecodesErrorDetails(t *testing.T) {
	got := parseBridgeDetails(`{"errorCode":"INSUFFICIENT_SCOPE","protonCode":9101}`)
	if got["errorCode"] != "INSUFFICIENT_SCOPE" || got["protonCode"] != "9101" {
		t.Fatalf("details parsed as %v", got)
	}
	if got := parseBridgeDetails("not json"); got != nil {
		t.Fatalf("invalid details parsed as %v", got)
	}
}
