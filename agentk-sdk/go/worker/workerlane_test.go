package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWorkerLane(t *testing.T) {
	for _, test := range []struct {
		stage string
		lane  string
		want  string
	}{
		{stage: "STAGING"},
		{stage: "PROD"},
		{stage: "PROD_REGRESSION_TEST"},
		{stage: "PROD_REGRESSION_TEST", lane: "PRT", want: "PRT"},
		{stage: "PROD_REGRESSION_TEST", lane: "dev_example", want: "dev_example"},
		{stage: "STAGING", lane: "dev_example", want: "dev_example"},
		{stage: "STAGING", lane: "prt", want: "prt"},
	} {
		t.Run(test.stage+"/"+test.lane, func(t *testing.T) {
			t.Setenv(workerLaneEnv, test.lane)
			t.Setenv(workerStageEnv, test.stage)
			got := readWorkerLane("demo")
			wantQueue := "demo"
			if test.want != "" {
				wantQueue += "@" + test.want
			}
			if got.err != nil || got.lane != test.want || got.pollQueue != wantQueue || got.baseQueue != "demo" {
				t.Fatalf("worker lane = %+v; want lane %q, queue %q", got, test.want, wantQueue)
			}
		})
	}
	t.Run("invalid", func(t *testing.T) {
		t.Setenv(workerLaneEnv, "dev lane")
		t.Setenv(workerStageEnv, "PROD_REGRESSION_TEST")
		got := readWorkerLane("demo")
		if got.err == nil || !strings.Contains(got.err.Error(), workerLaneEnv) {
			t.Fatalf("worker lane = %+v", got)
		}
	})
}

func TestInstanceLaneResolution(t *testing.T) {
	t.Run("explicit lane wins over managed stage", func(t *testing.T) {
		lane, err := instanceLaneFrom("dev_example", "STAGING", "", "")
		if err != nil || lane != "dev_example" {
			t.Fatalf("lane = %q, %v", lane, err)
		}
	})
	t.Run("managed without lane is baseline", func(t *testing.T) {
		lane, err := instanceLaneFrom("", "STAGING", "", "")
		if err != nil || lane != "" {
			t.Fatalf("lane = %q, %v", lane, err)
		}
	})
	t.Run("blank stage is local", func(t *testing.T) {
		lane, err := instanceLaneFrom("", " \t", "Yuchen", "dev-host")
		if err != nil || !strings.HasPrefix(lane, "local-yuchen-") {
			t.Fatalf("lane = %q, %v", lane, err)
		}
	})
	for _, missing := range []struct {
		name     string
		username string
		hostname string
	}{
		{name: "username", hostname: "dev-host"},
		{name: "hostname", username: "Yuchen"},
	} {
		t.Run("missing "+missing.name, func(t *testing.T) {
			if _, err := instanceLaneFrom("", "", missing.username, missing.hostname); err == nil ||
				!strings.Contains(err.Error(), workerLaneEnv) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLocalInstanceLaneSharedFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "protocol", "agentk", "worker_lane", "v1", "testdata", "worker-instance-lanes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Username string `json:"username"`
			Hostname string `json:"hostname"`
			LaneID   string `json:"laneId"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, test := range fixture.Cases {
		lane, err := localInstanceLane(test.Username, test.Hostname)
		if err != nil || lane != test.LaneID {
			t.Errorf("localInstanceLane(%q, %q) = %q, %v; want %q",
				test.Username, test.Hostname, lane, err, test.LaneID)
		}
	}
}

func TestWorkerLaneKeepsCase(t *testing.T) {
	t.Setenv("KWS_LANE_ID", "PRT")
	t.Setenv("KWS_SERVICE_STAGE", "PROD_REGRESSION_TEST")
	got := readWorkerLane("agentk")
	if got.err != nil || got.lane != "PRT" || got.pollQueue != "agentk@PRT" {
		t.Fatalf("%+v", got)
	}
}
