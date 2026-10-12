package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/crossplane/upjet/pkg/terraform"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/linode/terraform-provider-linode/v2/linode"

	"github.com/linode/provider-linode/apis/v1beta1"
)

func TestPrepareTerraformProviderConfigurationEventPoll(t *testing.T) {
	for _, tc := range []struct {
		name       string
		eventPoll  int
		retryDelay int
	}{
		{name: "poll without retry override", eventPoll: 15000},
		{name: "independent poll and retry", eventPoll: 15000, retryDelay: 2000},
		{name: "retry without poll override", retryDelay: 2000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := prepareTerraformProviderConfiguration(nil, v1beta1.ProviderConfiguration{
				EventPollms:     tc.eventPoll,
				MaxRetryDelayms: tc.retryDelay,
			})
			if got, ok := config["event_poll_ms"]; tc.eventPoll == 0 {
				if ok {
					t.Errorf("event_poll_ms = %v, want no override", got)
				}
			} else if !ok || got != tc.eventPoll {
				t.Errorf("event_poll_ms = %v, want %d", got, tc.eventPoll)
			}
			if tc.retryDelay > 0 && config["max_retry_delay_ms"] != tc.retryDelay {
				t.Errorf("max_retry_delay_ms = %v, want %d", config["max_retry_delay_ms"], tc.retryDelay)
			}
		})
	}
}

func TestConfigureNoForkLinodeClientAvoidsRedundantRecordRequests(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v4/domains/42/records/17" {
			t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":17,"name":"www","type":"A","target":"192.0.2.1"}`))
	}))
	t.Cleanup(server.Close)
	setup := terraform.Setup{Configuration: map[string]any{
		"token": "test-token",
		"url":   server.URL,
	}}
	provider := linode.Provider()
	if err := configureNoForkLinodeclient(context.Background(), &setup, *provider); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("configuration made %d API requests, want 0", got)
	}
	record := provider.ResourcesMap["linode_domain_record"]
	data := schema.TestResourceDataRaw(t, record.Schema, map[string]any{
		"domain_id":   42,
		"name":        "www",
		"record_type": "A",
		"target":      "192.0.2.1",
	})
	data.SetId("17")
	if diagnostics := record.ReadContext(context.Background(), data, setup.Meta); diagnostics.HasError() {
		t.Fatalf("record read failed: %v", diagnostics)
	}
	if data.Get("name") != "www" || requests.Load() != 1 {
		t.Fatalf("record name = %v, API requests = %d; want www and 1", data.Get("name"), requests.Load())
	}
}
