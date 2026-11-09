package gateway

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	inferpb "git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/gen/infer"
	"git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Inferer interface {
	Infer(ctx context.Context, serviceName, lane string, request *inferpb.InferRequest) (*inferpb.InferResponse, error)
}

type Options struct {
	Routes         map[string]config.Route
	MaxRequestSize int64
	BearerToken    string
	Logger         *slog.Logger
}

type Handler struct {
	inferer        Inferer
	routes         map[string]config.Route
	maxRequestSize int64
	bearerToken    string
	logger         *slog.Logger
}

type inferRequest struct {
	RequestID string                     `json:"requestId,omitempty"`
	ModelName string                     `json:"modelName,omitempty"`
	Features  map[string]json.RawMessage `json:"features"`
}

type inferResponse struct {
	RequestID string         `json:"requestId"`
	Route     string         `json:"route"`
	Features  map[string]any `json:"features"`
	LatencyMS int64          `json:"latencyMs"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	RequestID    string `json:"requestId,omitempty"`
	UpstreamCode int64  `json:"upstreamCode,omitempty"`
}

func New(inferer Inferer, options Options) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	handler := &Handler{
		inferer: inferer, routes: options.Routes, maxRequestSize: options.MaxRequestSize,
		bearerToken: options.BearerToken, logger: logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.health)
	mux.HandleFunc("GET /readyz", handler.health)
	mux.HandleFunc("GET /v1/routes", handler.authorize(handler.listRoutes))
	mux.HandleFunc("POST /v1/infer/{route}", handler.authorize(handler.infer))
	return mux
}

func (h *Handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) listRoutes(writer http.ResponseWriter, _ *http.Request) {
	type routeDescription struct {
		Name            string   `json:"name"`
		TimeoutSeconds  int      `json:"timeoutSeconds"`
		AllowedFeatures []string `json:"allowedFeatures,omitempty"`
	}
	routes := make([]routeDescription, 0, len(h.routes))
	for name, route := range h.routes {
		features := append([]string(nil), route.AllowedFeatures...)
		sort.Strings(features)
		routes = append(routes, routeDescription{Name: name, TimeoutSeconds: route.TimeoutSeconds, AllowedFeatures: features})
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Name < routes[j].Name })
	writeJSON(writer, http.StatusOK, map[string]any{"routes": routes})
}

func (h *Handler) infer(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	routeName := request.PathValue("route")
	route, ok := h.routes[routeName]
	if !ok {
		writeError(writer, http.StatusNotFound, "ROUTE_NOT_FOUND", "unknown MaaS route", "", 0)
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, h.maxRequestSize)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input inferRequest
	if err := decoder.Decode(&input); err != nil {
		writeDecodeError(writer, err)
		return
	}
	if err := expectEOF(decoder); err != nil {
		writeError(writer, http.StatusBadRequest, "INVALID_JSON", err.Error(), "", 0)
		return
	}
	if input.Features == nil {
		writeError(writer, http.StatusBadRequest, "INVALID_REQUEST", "features is required", input.RequestID, 0)
		return
	}
	for name := range input.Features {
		if !route.AllowsFeature(name) {
			writeError(writer, http.StatusBadRequest, "FEATURE_NOT_ALLOWED", fmt.Sprintf("feature %q is not allowed for route %q", name, routeName), input.RequestID, 0)
			return
		}
	}

	requestID := strings.TrimSpace(input.RequestID)
	idempotencyKey := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if requestID != "" && idempotencyKey != "" && requestID != idempotencyKey {
		writeError(writer, http.StatusBadRequest, "IDEMPOTENCY_KEY_MISMATCH", "Idempotency-Key must match requestId", requestID, 0)
		return
	}
	if idempotencyKey != "" {
		requestID = idempotencyKey
	}
	if requestID == "" {
		requestID = newRequestID()
	}
	if len(requestID) > 128 {
		writeError(writer, http.StatusBadRequest, "INVALID_REQUEST_ID", "requestId and Idempotency-Key must be at most 128 characters", "", 0)
		return
	}
	writer.Header().Set("Idempotency-Key", requestID)
	mergedFeatures := make(map[string]json.RawMessage,
		len(route.DefaultFeatures)+len(input.Features))
	for name, value := range route.DefaultFeatures {
		mergedFeatures[name] = value
	}
	// Caller values deliberately override route defaults. This keeps generic routes
	// usable while letting narrow adapters omit deployment-owned features.
	for name, value := range input.Features {
		mergedFeatures[name] = value
	}
	features, err := decodeFeatures(mergedFeatures)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "INVALID_FEATURE", err.Error(), requestID, 0)
		return
	}
	features["request_id"] = stringFeature("request_id", requestID)
	features["start_time"] = intFeature("start_time", started.UnixMilli())

	ctx, cancel := context.WithTimeout(request.Context(), route.Timeout())
	defer cancel()
	response, err := h.inferer.Infer(ctx, route.ServiceName, route.Lane, &inferpb.InferRequest{
		RequestId: requestID, ModelName: input.ModelName, Feature: features,
	})
	if err != nil {
		statusCode, code := mapRPCError(err)
		h.logger.Error("MaaS RPC failed", "route", routeName, "request_id", requestID, "latency_ms", time.Since(started).Milliseconds(), "error", err)
		writeError(writer, statusCode, code, publicRPCMessage(err), requestID, 0)
		return
	}
	if response == nil {
		h.logger.Error("MaaS RPC returned nil", "route", routeName, "request_id", requestID)
		writeError(writer, http.StatusBadGateway, "UPSTREAM_INVALID_RESPONSE", "MaaS service returned an empty response", requestID, 0)
		return
	}
	if upstreamCode := featureInt(response.GetFeature()["error_code"]); upstreamCode != 0 {
		message := firstFeatureString(response.GetFeature(), "error_message", "error_msg")
		if message == "" {
			message = "MaaS service reported a business error"
		}
		h.logger.Warn("MaaS business error", "route", routeName, "request_id", requestID, "upstream_code", upstreamCode, "latency_ms", time.Since(started).Milliseconds())
		writeError(writer, http.StatusBadGateway, "UPSTREAM_ERROR", truncate(message, 1024), requestID, upstreamCode)
		return
	}

	latency := time.Since(started).Milliseconds()
	h.logger.Info("MaaS RPC completed", "route", routeName, "request_id", requestID, "latency_ms", latency)
	writeJSON(writer, http.StatusOK, inferResponse{
		RequestID: requestID, Route: routeName, Features: encodeFeatures(response.GetFeature()), LatencyMS: latency,
	})
}

func (h *Handler) authorize(next http.HandlerFunc) http.HandlerFunc {
	if h.bearerToken == "" {
		return next
	}
	expected := []byte("Bearer " + h.bearerToken)
	return func(writer http.ResponseWriter, request *http.Request) {
		provided := []byte(request.Header.Get("Authorization"))
		if len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
			writer.Header().Set("WWW-Authenticate", "Bearer")
			writeError(writer, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid bearer token", "", 0)
			return
		}
		next(writer, request)
	}
}

func stringFeature(name, value string) *inferpb.ExFeature {
	return &inferpb.ExFeature{Name: name, Kind: &inferpb.ExFeature_StrVal{StrVal: value}}
}

func intFeature(name string, value int64) *inferpb.ExFeature {
	return &inferpb.ExFeature{Name: name, Kind: &inferpb.ExFeature_IntVal{IntVal: value}}
}

func featureInt(feature *inferpb.ExFeature) int64 {
	if feature == nil {
		return 0
	}
	switch value := feature.Kind.(type) {
	case *inferpb.ExFeature_IntVal:
		return value.IntVal
	case *inferpb.ExFeature_Int32Val:
		return int64(value.Int32Val)
	default:
		return 0
	}
}

func firstFeatureString(features map[string]*inferpb.ExFeature, names ...string) string {
	for _, name := range names {
		if feature := features[name]; feature != nil {
			if value, ok := feature.Kind.(*inferpb.ExFeature_StrVal); ok {
				return value.StrVal
			}
		}
	}
	return ""
}

func newRequestID() string {
	var buffer [12]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return fmt.Sprintf("maas_%d", time.Now().UnixNano())
	}
	return "maas_" + hex.EncodeToString(buffer[:])
}

func mapRPCError(err error) (int, string) {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		return http.StatusBadRequest, "UPSTREAM_REJECTED"
	case codes.Unauthenticated:
		return http.StatusBadGateway, "UPSTREAM_UNAUTHENTICATED"
	case codes.PermissionDenied:
		return http.StatusBadGateway, "UPSTREAM_PERMISSION_DENIED"
	case codes.NotFound:
		return http.StatusBadGateway, "UPSTREAM_NOT_FOUND"
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests, "UPSTREAM_RATE_LIMITED"
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT"
	case codes.Canceled:
		return http.StatusRequestTimeout, "REQUEST_CANCELED"
	case codes.Unavailable:
		return http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE"
	default:
		return http.StatusBadGateway, "UPSTREAM_RPC_ERROR"
	}
}

func publicRPCMessage(err error) string {
	switch status.Code(err) {
	case codes.DeadlineExceeded:
		return "MaaS request timed out"
	case codes.ResourceExhausted:
		return "MaaS service is rate limited or exhausted"
	case codes.Unavailable:
		return "MaaS service is unavailable"
	case codes.Canceled:
		return "request was canceled"
	default:
		return "MaaS RPC failed"
	}
}

func writeDecodeError(writer http.ResponseWriter, err error) {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		writeError(writer, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request body exceeds configured limit", "", 0)
		return
	}
	if errors.Is(err, io.EOF) {
		writeError(writer, http.StatusBadRequest, "INVALID_JSON", "request body is required", "", 0)
		return
	}
	writeError(writer, http.StatusBadRequest, "INVALID_JSON", err.Error(), "", 0)
}

func writeError(writer http.ResponseWriter, statusCode int, code, message, requestID string, upstreamCode int64) {
	writeJSON(writer, statusCode, errorEnvelope{Error: errorBody{
		Code: code, Message: message, RequestID: requestID, UpstreamCode: upstreamCode,
	}})
}

func writeJSON(writer http.ResponseWriter, statusCode int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
