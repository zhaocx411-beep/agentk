package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"git.corp.kuaishou.com/infra/infra-framework-go.git/kconf"
)

const (
	defaultHTTPAddress    = ":8080"
	defaultMaxRequestSize = int64(16 << 20)
	defaultShutdown       = 30 * time.Second
	maxRouteTimeout       = time.Hour
)

var (
	routeNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	serviceNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,255}$`)
	featureNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	capabilityIDPattern = regexp.MustCompile(`^contrib\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
	artifactTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	taskQueuePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,255}$`)
)

// GatewayConfig 只包含 PRT HTTP Gateway 所需的配置。
type GatewayConfig struct {
	HTTPAddress    string
	MaxRequestSize int64
	Shutdown       time.Duration
	BearerToken    string
	Routes         map[string]Route
}

// Route 是 PRT 内部 HTTP route 到 KESS RPC 的映射，不会被 Staging Worker 加载。
type Route struct {
	ServiceName     string                     `json:"serviceName"`
	TimeoutSeconds  int                        `json:"timeoutSeconds"`
	Lane            string                     `json:"lane,omitempty"`
	AllowedFeatures []string                   `json:"allowedFeatures,omitempty"`
	DefaultFeatures map[string]json.RawMessage `json:"defaultFeatures,omitempty"`

	allowed map[string]struct{}
}

// WorkerConfig 只包含 Staging AgentK Worker 所需的配置。
type WorkerConfig struct {
	HealthAddress  string
	Shutdown       time.Duration
	TaskQueue      string
	GatewayBaseURL *url.URL
	GatewayToken   string
	Capabilities   map[string]CapabilityRoute
}

// CapabilityRoute 描述公开给 AgentK 的窄契约。map key 是稳定的 Activity 注册名；
// gatewayRoute 省略时 map key 同时也是 Gateway route；adapter 只做协议转换，
// 所有推理和媒体处理仍由 Gateway 后面的 MaaS 服务执行。
type CapabilityRoute struct {
	TimeoutSeconds  int               `json:"timeoutSeconds"`
	GatewayRoute    string            `json:"gatewayRoute,omitempty"`
	Adapter         string            `json:"adapter,omitempty"`
	ArtifactOutputs map[string]string `json:"artifactOutputs,omitempty"`
	AgentK          AgentKCapability  `json:"agentk"`
}

type AgentKCapability struct {
	ID              string          `json:"id"`
	Description     string          `json:"description"`
	ToolDescription string          `json:"toolDescription,omitempty"`
	InputSchema     json.RawMessage `json:"inputSchema"`
	OutputSchema    json.RawMessage `json:"outputSchema"`
	MaxAttempts     int32           `json:"maxAttempts,omitempty"`
}

func (r Route) Timeout() time.Duration {
	return time.Duration(r.TimeoutSeconds) * time.Second
}

func (r CapabilityRoute) Timeout() time.Duration {
	return time.Duration(r.TimeoutSeconds) * time.Second
}

func (r Route) AllowsFeature(name string) bool {
	if name == "request_id" || name == "start_time" {
		return true
	}
	if len(r.allowed) == 0 {
		return true
	}
	_, ok := r.allowed[name]
	return ok
}

type routesDocument struct {
	Routes map[string]Route `json:"routes"`
}

type capabilitiesDocument struct {
	Capabilities map[string]CapabilityRoute `json:"capabilities"`
}

var getKConfString = func(key string) (string, error) {
	value, err := kconf.GetStringConfig(key)
	return string(value), err
}

func LoadGateway() (GatewayConfig, error) {
	routes, err := loadGatewayRoutes()
	if err != nil {
		return GatewayConfig{}, err
	}
	maxRequestSize, err := envInt64("MAAS_MAX_REQUEST_BYTES", defaultMaxRequestSize)
	if err != nil {
		return GatewayConfig{}, err
	}
	if maxRequestSize <= 0 || maxRequestSize > 128<<20 {
		return GatewayConfig{}, fmt.Errorf("MAAS_MAX_REQUEST_BYTES must be in (0, %d]", 128<<20)
	}
	shutdown, err := loadShutdown()
	if err != nil {
		return GatewayConfig{}, err
	}
	address := strings.TrimSpace(os.Getenv("MAAS_HTTP_ADDR"))
	if address == "" {
		address = defaultHTTPAddress
	}
	return GatewayConfig{
		HTTPAddress: address, MaxRequestSize: maxRequestSize, Shutdown: shutdown,
		BearerToken: os.Getenv("MAAS_GATEWAY_BEARER_TOKEN"), Routes: routes,
	}, nil
}

func LoadWorker() (WorkerConfig, error) {
	capabilities, err := loadCapabilities()
	if err != nil {
		return WorkerConfig{}, err
	}
	shutdown, err := loadShutdown()
	if err != nil {
		return WorkerConfig{}, err
	}
	healthAddress := strings.TrimSpace(os.Getenv("MAAS_HTTP_ADDR"))
	if healthAddress == "" {
		healthAddress = defaultHTTPAddress
	}
	identityService := strings.TrimSpace(os.Getenv("KWS_SERVICE_NAME"))
	if !serviceNamePattern.MatchString(identityService) {
		return WorkerConfig{}, errors.New("Staging worker requires KWS_SERVICE_NAME for AgentK Temporal identity")
	}
	taskQueue := strings.TrimSpace(os.Getenv("MAAS_AGENTK_TASK_QUEUE"))
	if taskQueue == "" {
		taskQueue = identityService + "-agentk"
	}
	if !taskQueuePattern.MatchString(taskQueue) {
		return WorkerConfig{}, fmt.Errorf("MAAS_AGENTK_TASK_QUEUE %q is invalid", taskQueue)
	}
	baseURL, err := parseGatewayBaseURL(os.Getenv("MAAS_GATEWAY_BASE_URL"))
	if err != nil {
		return WorkerConfig{}, err
	}
	return WorkerConfig{
		HealthAddress: healthAddress, Shutdown: shutdown,
		TaskQueue: taskQueue, GatewayBaseURL: baseURL,
		GatewayToken: os.Getenv("MAAS_GATEWAY_BEARER_TOKEN"),
		Capabilities: capabilities,
	}, nil
}

func loadGatewayRoutes() (map[string]Route, error) {
	raw, err := configJSON("MAAS_ROUTES_KCONF_KEY", "MAAS_ROUTES_JSON", "MAAS_ROUTES_FILE", "configs/routes.json")
	if err != nil {
		return nil, err
	}
	var document routesDocument
	if err := decodeStrict(raw, &document); err != nil {
		return nil, fmt.Errorf("decode MaaS routes: %w", err)
	}
	if len(document.Routes) == 0 {
		return nil, errors.New("MaaS routes must contain at least one route")
	}
	for name, route := range document.Routes {
		if !routeNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid route name %q", name)
		}
		if !serviceNamePattern.MatchString(route.ServiceName) {
			return nil, fmt.Errorf("route %q has invalid serviceName %q", name, route.ServiceName)
		}
		if err := validateTimeout(name, route.TimeoutSeconds); err != nil {
			return nil, err
		}
		route.allowed = make(map[string]struct{}, len(route.AllowedFeatures))
		for _, feature := range route.AllowedFeatures {
			if !featureNamePattern.MatchString(feature) {
				return nil, fmt.Errorf("route %q has invalid allowed feature %q", name, feature)
			}
			if _, exists := route.allowed[feature]; exists {
				return nil, fmt.Errorf("route %q repeats allowed feature %q", name, feature)
			}
			route.allowed[feature] = struct{}{}
		}
		for feature, raw := range route.DefaultFeatures {
			if !featureNamePattern.MatchString(feature) || feature == "request_id" || feature == "start_time" {
				return nil, fmt.Errorf("route %q has invalid default feature %q", name, feature)
			}
			if !route.AllowsFeature(feature) {
				return nil, fmt.Errorf(
					"route %q default feature %q is not in allowedFeatures", name, feature)
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil || value == nil {
				return nil, fmt.Errorf(
					"route %q default feature %q must be a non-null JSON value", name, feature)
			}
		}
		document.Routes[name] = route
	}
	return document.Routes, nil
}

func loadCapabilities() (map[string]CapabilityRoute, error) {
	raw, err := configJSON("MAAS_CAPABILITIES_KCONF_KEY", "MAAS_CAPABILITIES_JSON", "MAAS_CAPABILITIES_FILE", "configs/capabilities.json")
	if err != nil {
		return nil, err
	}
	var document capabilitiesDocument
	if err := decodeStrict(raw, &document); err != nil {
		return nil, fmt.Errorf("decode AgentK capabilities: %w", err)
	}
	if len(document.Capabilities) == 0 {
		return nil, errors.New("AgentK capabilities must contain at least one capability")
	}
	capabilityRoutes := make(map[string]string)
	activityRoutes := make(map[string]string)
	for activityName, route := range document.Capabilities {
		if !routeNamePattern.MatchString(activityName) {
			return nil, fmt.Errorf("invalid capability route name %q", activityName)
		}
		if err := validateTimeout(activityName, route.TimeoutSeconds); err != nil {
			return nil, err
		}
		gatewayRoute := route.GatewayRoute
		if gatewayRoute == "" {
			gatewayRoute = activityName
		}
		if !routeNamePattern.MatchString(gatewayRoute) {
			return nil, fmt.Errorf(
				"capability route %q has invalid gatewayRoute %q", activityName, gatewayRoute)
		}
		switch route.Adapter {
		case "", "process_video", "compose_video", "extract_audio":
		default:
			return nil, fmt.Errorf(
				"capability route %q has unsupported adapter %q", activityName, route.Adapter)
		}
		if route.Adapter != "" && route.GatewayRoute == "" {
			return nil, fmt.Errorf(
				"capability route %q with adapter %q requires gatewayRoute",
				activityName, route.Adapter)
		}
		for feature, artifactType := range route.ArtifactOutputs {
			if !featureNamePattern.MatchString(feature) {
				return nil, fmt.Errorf(
					"capability route %q has invalid artifact output feature %q",
					activityName, feature)
			}
			if feature == "artifacts" {
				return nil, fmt.Errorf(
					"capability route %q cannot use reserved artifact output feature %q",
					activityName, feature)
			}
			if !artifactTypePattern.MatchString(artifactType) {
				return nil, fmt.Errorf(
					"capability route %q artifact output %q has invalid type %q",
					activityName, feature, artifactType)
			}
		}
		if err := validateAgentKCapability(activityName, &route.AgentK); err != nil {
			return nil, err
		}
		if previous, exists := capabilityRoutes[route.AgentK.ID]; exists {
			return nil, fmt.Errorf("capability routes %q and %q repeat agentk.id %q", previous, activityName, route.AgentK.ID)
		}
		capabilityRoutes[route.AgentK.ID] = activityName
		activityType := AgentKActivityType(activityName)
		if previous, exists := activityRoutes[activityType]; exists {
			return nil, fmt.Errorf("capability routes %q and %q map to the same AgentK activity type %q", previous, activityName, activityType)
		}
		activityRoutes[activityType] = activityName
	}
	return document.Capabilities, nil
}

func configJSON(kconfEnv, jsonEnv, fileEnv, defaultPath string) (string, error) {
	if key := strings.TrimSpace(os.Getenv(kconfEnv)); key != "" {
		raw, err := getKConfString(key)
		if err != nil {
			return "", fmt.Errorf("read KConf %q from %s: %w", key, kconfEnv, err)
		}
		if strings.TrimSpace(raw) == "" {
			return "", fmt.Errorf("KConf %q from %s is empty", key, kconfEnv)
		}
		return raw, nil
	}
	if raw := strings.TrimSpace(os.Getenv(jsonEnv)); raw != "" {
		return raw, nil
	}
	path := strings.TrimSpace(os.Getenv(fileEnv))
	if path == "" {
		path = defaultPath
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read configuration %q: %w", path, err)
	}
	return string(contents), nil
}

func decodeStrict(raw string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return expectJSONEOF(decoder)
}

func validateTimeout(routeName string, seconds int) error {
	if seconds <= 0 || time.Duration(seconds)*time.Second > maxRouteTimeout {
		return fmt.Errorf("route %q timeoutSeconds must be in (0, %d]", routeName, int(maxRouteTimeout/time.Second))
	}
	return nil
}

// AgentKActivityType 是写入 Temporal History 的稳定 ABI；route 改名视为不兼容变更。
func AgentKActivityType(routeName string) string {
	return "maas.infer." + strings.ReplaceAll(routeName, "-", "_") + ".v1"
}

func validateAgentKCapability(routeName string, capability *AgentKCapability) error {
	if !capabilityIDPattern.MatchString(capability.ID) || len(capability.ID) > 128 {
		return fmt.Errorf("route %q agentk.id %q must match contrib.<owner>.<name>", routeName, capability.ID)
	}
	if strings.TrimSpace(capability.Description) == "" {
		return fmt.Errorf("route %q agentk.description is required", routeName)
	}
	if capability.MaxAttempts < 0 {
		return fmt.Errorf("route %q agentk.maxAttempts must be non-negative", routeName)
	}
	if err := validateSchema(capability.InputSchema, true); err != nil {
		return fmt.Errorf("route %q agentk.inputSchema: %w", routeName, err)
	}
	if err := validateSchema(capability.OutputSchema, false); err != nil {
		return fmt.Errorf("route %q agentk.outputSchema: %w", routeName, err)
	}
	return nil
}

func validateSchema(raw json.RawMessage, requireObject bool) error {
	if len(raw) == 0 {
		return errors.New("schema is required")
	}
	var schema map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if err := decoder.Decode(&schema); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if schema == nil {
		return errors.New("schema must be a JSON object")
	}
	if err := expectJSONEOF(decoder); err != nil {
		return err
	}
	if requireObject && schema["type"] != "object" {
		return errors.New(`schema type must be "object"`)
	}
	return nil
}

func expectJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return errors.New("multiple JSON values are not allowed")
}

func parseGatewayBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("Staging worker requires MAAS_GATEWAY_BASE_URL")
	}
	baseURL, err := url.Parse(raw)
	if err != nil || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("MAAS_GATEWAY_BASE_URL %q must be an absolute URL without credentials, query, or fragment", raw)
	}
	allowHTTP, err := envOnOff("MAAS_GATEWAY_ALLOW_INSECURE_HTTP", false)
	if err != nil {
		return nil, err
	}
	if baseURL.Scheme != "https" && !(allowHTTP && baseURL.Scheme == "http") {
		return nil, errors.New("MAAS_GATEWAY_BASE_URL must use https (set MAAS_GATEWAY_ALLOW_INSECURE_HTTP=on only for local tests)")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	return baseURL, nil
}

func loadShutdown() (time.Duration, error) {
	shutdown, err := envDuration("MAAS_SHUTDOWN_TIMEOUT", defaultShutdown)
	if err != nil {
		return 0, err
	}
	if shutdown <= 0 || shutdown > 5*time.Minute {
		return 0, errors.New("MAAS_SHUTDOWN_TIMEOUT must be in (0, 5m]")
	}
	return shutdown, nil
}

func envOnOff(name string, fallback bool) (bool, error) {
	switch value := strings.TrimSpace(os.Getenv(name)); value {
	case "":
		return fallback, nil
	case "on":
		return true, nil
	case "off":
		return false, nil
	default:
		return false, fmt.Errorf(`%s must be "on" or "off", got %q`, name, value)
	}
}

func envInt64(name string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return value, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", name, err)
	}
	return value, nil
}
