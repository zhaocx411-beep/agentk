package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"strings"

	ksrpc "git.corp.kuaishou.com/klingai-agentk/ksresolve.git/go/rpc"
)

const (
	workerLaneEnv  = "KWS_LANE_ID"
	workerStageEnv = "KWS_SERVICE_STAGE"
	localLaneSalt  = "agentk-local-lane-v1"
	localSlugLimit = 32
)

type workerLane struct {
	lane      string
	err       error
	baseQueue string
	pollQueue string
}

func readWorkerLane(baseQueue string) workerLane {
	configured := workerLane{baseQueue: baseQueue, pollQueue: baseQueue}
	lane, err := InstanceLaneFromEnv()
	if err != nil {
		configured.err = err
		return configured
	}
	configured.lane = lane
	configured.pollQueue = TaskQueueForLane(baseQueue, lane)
	return configured
}

// TaskQueueForLane returns the physical Temporal task queue a Worker with the
// given lane polls: the base queue itself for baseline, otherwise
// baseTaskQueue + "@" + lane.
func TaskQueueForLane(baseTaskQueue, lane string) string {
	if lane == "" {
		return baseTaskQueue
	}
	return baseTaskQueue + "@" + lane
}

// InstanceLaneFromEnv resolves the static lane identity for a Worker process.
// A configured KWS lane wins; otherwise KWS-managed processes use baseline and
// local processes use a stable isolated lane.
func InstanceLaneFromEnv() (string, error) {
	rawLane := os.Getenv(workerLaneEnv)
	serviceStage := os.Getenv(workerStageEnv)
	if rawLane != "" || strings.TrimSpace(serviceStage) != "" {
		return instanceLaneFrom(rawLane, serviceStage, "", "")
	}
	username := ""
	if current, err := user.Current(); err == nil {
		username = current.Username
	}
	hostname, _ := os.Hostname()
	return instanceLaneFrom(
		rawLane,
		serviceStage,
		username,
		hostname,
	)
}

func instanceLaneFrom(rawLane, serviceStage, username, hostname string) (string, error) {
	if rawLane != "" {
		if err := ksrpc.ValidateLane(rawLane); err != nil {
			return "", fmt.Errorf("%s: %w", workerLaneEnv, err)
		}
		return rawLane, nil
	}
	if strings.TrimSpace(serviceStage) != "" {
		return "", nil
	}
	return localInstanceLane(username, hostname)
}

func localInstanceLane(username, hostname string) (string, error) {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(hostname) == "" {
		return "", fmt.Errorf(
			"cannot derive local worker lane: operating-system username and hostname are required; set %s explicitly",
			workerLaneEnv,
		)
	}
	slug := localUsernameSlug(username)
	digest := sha256.Sum256([]byte(localLaneSalt + "\x00" + username + "\x00" + hostname))
	lane := "local-" + slug + "-" + hex.EncodeToString(digest[:])[:12]
	if err := ksrpc.ValidateLane(lane); err != nil {
		return "", fmt.Errorf("derive local worker lane: %w", err)
	}
	return lane, nil
}

func localUsernameSlug(username string) string {
	var slug strings.Builder
	separator := false
	for _, char := range strings.ToLower(username) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			slug.WriteRune(char)
			separator = false
			continue
		}
		if slug.Len() > 0 && !separator {
			slug.WriteByte('-')
			separator = true
		}
	}
	value := strings.Trim(slug.String(), "-")
	if len(value) > localSlugLimit {
		value = strings.TrimRight(value[:localSlugLimit], "-")
	}
	if value == "" {
		return "user"
	}
	return value
}
