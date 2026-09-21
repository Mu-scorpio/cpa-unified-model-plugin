package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

//go:embed web/index.html
var managementPage []byte

const resourcePath = "/status"

// catalogQueryParam asks the resource page for the built-in model catalog instead of HTML.
const catalogQueryParam = "view"

// catalogQueryParamValue selects the built-in model name -> context window table.
const catalogQueryParamValue = "catalog"

var mappings = newMappingStore()

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type registrationCapability struct {
	ModelRegistrar        bool                         `json:"model_registrar"`
	ModelRouter           bool                         `json:"model_router"`
	Executor              bool                         `json:"executor"`
	ExecutorModelScope    pluginapi.ExecutorModelScope `json:"executor_model_scope"`
	ExecutorInputFormats  []string                     `json:"executor_input_formats,omitempty"`
	ExecutorOutputFormats []string                     `json:"executor_output_formats,omitempty"`
	RequestInterceptor    bool                         `json:"request_interceptor"`
	ResponseInterceptor   bool                         `json:"response_interceptor"`
	ManagementAPI         bool                         `json:"management_api"`
}

type identifierResponse struct {
	Identifier string `json:"identifier"`
}

type rpcModelRouteRequest struct {
	pluginapi.ModelRouteRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type rpcRequestInterceptRequest struct {
	pluginapi.RequestInterceptRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type rpcResponseInterceptRequest struct {
	pluginapi.ResponseInterceptRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type rpcExecutorRequest struct {
	pluginapi.ExecutorRequest
	StreamID       string `json:"stream_id,omitempty"`
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type hostModelExecutionRequest struct {
	pluginapi.HostModelExecutionRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type managementRegistration struct {
	Resources []managementResource `json:"resources,omitempty"`
}

type managementResource struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description"`
}

type managementRequest struct {
	Method         string
	Path           string
	Headers        http.Header
	Query          map[string][]string
	Body           []byte
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}

type streamResponse struct {
	Headers http.Header                     `json:"headers,omitempty"`
	Chunks  []pluginapi.ExecutorStreamChunk `json:"chunks,omitempty"`
}

type rpcStreamEmitRequest struct {
	StreamID string `json:"stream_id"`
	Payload  []byte `json:"payload,omitempty"`
	Error    string `json:"error,omitempty"`
}

type rpcStreamCloseRequest struct {
	StreamID string `json:"stream_id"`
	Error    string `json:"error,omitempty"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}
	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleMethod(C.GoString(method), requestBytes)
	if errHandle != nil {
		writeResponse(response, errorEnvelope("plugin_error", errHandle.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, length C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = length
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errConfig := applyLifecycleConfig(request); errConfig != nil {
			return nil, errConfig
		}
		return okEnvelope(pluginRegistration())
	case pluginabi.MethodPluginQuiesce, pluginabi.MethodPluginShutdown:
		return okEnvelope(map[string]any{})
	case pluginabi.MethodModelRegister:
		return okEnvelope(pluginapi.ModelRegistrationResponse{
			Provider: providerID,
			Models:   modelInfos(mappings.snapshot(), mappings.snapshotContextLengths()),
		})
	case pluginabi.MethodModelRoute:
		return handleModelRoute(request)
	case pluginabi.MethodRequestInterceptBefore, pluginabi.MethodRequestInterceptAfter:
		return handleRequestIntercept(request)
	case pluginabi.MethodResponseInterceptAfter:
		return handleResponseIntercept(request)
	case pluginabi.MethodExecutorIdentifier:
		return okEnvelope(identifierResponse{Identifier: providerID})
	case pluginabi.MethodExecutorExecute:
		return handleExecutorExecute(request)
	case pluginabi.MethodExecutorExecuteStream:
		return handleExecutorExecuteStream(request)
	case pluginabi.MethodExecutorCountTokens:
		return handleExecutorCountTokens(request)
	case pluginabi.MethodManagementRegister:
		return okEnvelope(managementRegistration{Resources: []managementResource{{
			Path:        resourcePath,
			Menu:        "统一模型",
			Description: "配置客户端模型别名到实际模型的映射。",
		}}})
	case pluginabi.MethodManagementHandle:
		return handleManagement(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func applyLifecycleConfig(raw []byte) error {
	if len(raw) == 0 {
		return mappings.replaceConfig(nil)
	}
	var req lifecycleRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return fmt.Errorf("decode plugin lifecycle request: %w", errUnmarshal)
	}
	return mappings.replaceConfig(req.ConfigYAML)
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             "统一模型",
			Version:          "0.4.1",
			Author:           "CLIProxyAPI",
			GitHubRepository: "https://github.com/router-for-me/CLIProxyAPI",
			ConfigFields: []pluginapi.ConfigField{
				{
					Name:        "mappings",
					Type:        pluginapi.ConfigFieldTypeArray,
					Description: "Client-visible model aliases, target model IDs, and optional built-in provider keys. Setting provider enables direct routing without a nested plugin execution callback.",
				},
				{
					Name:        "mappings[].auth",
					Type:        pluginapi.ConfigFieldTypeString,
					Description: "Optional credential ID (auth file name) that pins execution to one credential of the provider. Pinned mappings run through the plugin executor with a forced provider and auth ID; leave empty for native provider routing.",
				},
				{
					Name:        "mappings[].context-length",
					Type:        pluginapi.ConfigFieldTypeInteger,
					Description: "Context window, in tokens, advertised to clients for the alias. Omitted or non-positive values fall back to the context length table and finally to 128000.",
				},
				{
					Name:        "context-lengths",
					Type:        pluginapi.ConfigFieldTypeObject,
					Description: "Overrides for the built-in model name -> context window table, keyed by model name or family, for example {gpt-5.5: 300000}. Used whenever an alias has no explicit context-length.",
				},
				{
					Name:        "mappings[].modalities",
					Type:        pluginapi.ConfigFieldTypeArray,
					EnumValues:  append([]string(nil), supportedModalities...),
					Description: "Input modalities advertised to clients for the alias. Defaults to [text] when omitted.",
				},
				{
					Name:        "enhanced-mode",
					Type:        pluginapi.ConfigFieldTypeBoolean,
					Description: "When true, client-facing model listings (OpenAI, Claude, Gemini, Codex client, Grok) only include plugin aliases. Native CPA models stay registered for routing but are hidden from clients.",
				},
			},
		},
		Capabilities: registrationCapability{
			ModelRegistrar:        true,
			ModelRouter:           true,
			Executor:              true,
			ExecutorModelScope:    pluginapi.ExecutorModelScopeBoth,
			ExecutorInputFormats:  []string{"chat-completions"},
			ExecutorOutputFormats: []string{"chat-completions"},
			RequestInterceptor:    true,
			ResponseInterceptor:   true,
			ManagementAPI:         true,
		},
	}
}

func handleModelRoute(raw []byte) ([]byte, error) {
	var req rpcModelRouteRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode model route request: %w", errUnmarshal)
	}
	if mapping, matched := mappings.resolveMapping(req.RequestedModel); matched {
		if strings.TrimSpace(mapping.Target) == "" {
			return okEnvelope(pluginapi.ModelRouteResponse{
				Handled:    true,
				TargetKind: pluginapi.ModelRouteTargetSelf,
				Reason:     "unified-model alias has no target configured",
			})
		}
		// A pinned credential must run through the plugin executor: the direct provider
		// route cannot carry an auth pin and would round-robin every credential.
		if mapping.pinnedAuthID() != "" {
			return okEnvelope(pluginapi.ModelRouteResponse{
				Handled:    true,
				TargetKind: pluginapi.ModelRouteTargetSelf,
				Reason:     "credential pinned by unified-model",
			})
		}
		if provider := strings.ToLower(strings.TrimSpace(mapping.Provider)); provider != "" && containsProvider(req.AvailableProviders, provider) {
			return okEnvelope(pluginapi.ModelRouteResponse{
				Handled:     true,
				TargetKind:  pluginapi.ModelRouteTargetProvider,
				Target:      provider,
				TargetModel: mapping.Target,
				Reason:      "direct built-in provider route mapped by unified-model",
			})
		}
		return okEnvelope(pluginapi.ModelRouteResponse{
			Handled:    true,
			TargetKind: pluginapi.ModelRouteTargetSelf,
			Reason:     "mapped by unified-model",
		})
	}
	return okEnvelope(pluginapi.ModelRouteResponse{Handled: false})
}

func handleRequestIntercept(raw []byte) ([]byte, error) {
	var req rpcRequestInterceptRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode request interceptor request: %w", errUnmarshal)
	}
	mapping, matched := mappings.resolveMapping(req.RequestedModel)
	if !matched || strings.TrimSpace(mapping.Provider) == "" || strings.TrimSpace(req.Model) == "" || strings.EqualFold(strings.TrimSpace(req.Model), strings.TrimSpace(req.RequestedModel)) {
		return okEnvelope(pluginapi.RequestInterceptResponse{})
	}
	if !strings.EqualFold(strings.TrimSpace(req.Model), strings.TrimSpace(mapping.Target)) {
		return okEnvelope(pluginapi.RequestInterceptResponse{})
	}
	if len(req.Body) == 0 {
		return okEnvelope(pluginapi.RequestInterceptResponse{})
	}
	body := rewriteRequestBody(req.Body, req.Model, req.Stream)
	if bytes.Equal(body, req.Body) {
		return okEnvelope(pluginapi.RequestInterceptResponse{})
	}
	return okEnvelope(pluginapi.RequestInterceptResponse{Body: body})
}

func handleResponseIntercept(raw []byte) ([]byte, error) {
	if !mappings.snapshotEnhancedMode() {
		return okEnvelope(pluginapi.ResponseInterceptResponse{})
	}
	var req rpcResponseInterceptRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return nil, fmt.Errorf("decode response interceptor request: %w", errUnmarshal)
		}
	}
	// Model listings are the only responses that carry an empty requested model and a catalog body.
	if strings.TrimSpace(req.RequestedModel) != "" || strings.TrimSpace(req.Model) != "" || req.Stream || len(req.Body) == 0 {
		return okEnvelope(pluginapi.ResponseInterceptResponse{})
	}
	aliases := aliasNameSet(mappings.snapshot())
	body, changed := filterClientModelListing(req.Body, aliases)
	if !changed {
		return okEnvelope(pluginapi.ResponseInterceptResponse{})
	}
	return okEnvelope(pluginapi.ResponseInterceptResponse{Body: body})
}

func containsProvider(providers []string, wanted string) bool {
	wanted = strings.ToLower(strings.TrimSpace(wanted))
	if wanted == "" {
		return false
	}
	for _, provider := range providers {
		if strings.EqualFold(strings.TrimSpace(provider), wanted) {
			return true
		}
	}
	return false
}

func handleExecutorExecute(raw []byte) ([]byte, error) {
	var req rpcExecutorRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode executor request: %w", errUnmarshal)
	}
	mapping, errTarget := resolveExecution(req.Model)
	if errTarget != nil {
		return nil, errTarget
	}
	hostRequest := hostModelRequestFromExecutor(req, mapping, false)
	result, errCall := callHost(pluginabi.MethodHostModelExecute, hostRequest)
	if errCall != nil {
		return nil, errCall
	}
	var response pluginapi.HostModelExecutionResponse
	if errUnmarshal := json.Unmarshal(result, &response); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host model response: %w", errUnmarshal)
	}
	return okEnvelope(pluginapi.ExecutorResponse{
		Payload: response.Body,
		Headers: response.Headers,
	})
}

func handleExecutorExecuteStream(raw []byte) ([]byte, error) {
	var req rpcExecutorRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode executor stream request: %w", errUnmarshal)
	}
	mapping, errTarget := resolveExecution(req.Model)
	if errTarget != nil {
		return nil, errTarget
	}
	hostRequest := hostModelRequestFromExecutor(req, mapping, true)
	result, errCall := callHost(pluginabi.MethodHostModelExecuteStream, hostRequest)
	if errCall != nil {
		return nil, errCall
	}
	var stream pluginapi.HostModelStreamResponse
	if errUnmarshal := json.Unmarshal(result, &stream); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host model stream response: %w", errUnmarshal)
	}
	if strings.TrimSpace(stream.StreamID) == "" {
		return nil, fmt.Errorf("host model stream response has no stream_id")
	}
	if strings.TrimSpace(req.StreamID) == "" {
		_ = closeHostStream(stream.StreamID)
		return nil, fmt.Errorf("executor stream_id is required")
	}
	if stream.StatusCode >= http.StatusBadRequest {
		_ = closeHostStream(stream.StreamID)
		return nil, fmt.Errorf("host model stream returned status %d", stream.StatusCode)
	}

	go forwardHostStream(stream.StreamID, req.StreamID)
	return okEnvelope(streamResponse{Headers: stream.Headers})
}

func handleExecutorCountTokens(raw []byte) ([]byte, error) {
	var req rpcExecutorRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode token count request: %w", errUnmarshal)
	}
	if _, errTarget := resolveExecution(req.Model); errTarget != nil {
		return nil, errTarget
	}
	payload, errMarshal := json.Marshal(map[string]int{"total_tokens": estimateTokens(req.Payload)})
	if errMarshal != nil {
		return nil, errMarshal
	}
	return okEnvelope(pluginapi.ExecutorResponse{Payload: payload})
}

// resolveExecution returns the mapping a model alias resolves to, with a valid target.
func resolveExecution(model string) (modelMapping, error) {
	mapping, matched := mappings.resolveMapping(model)
	if !matched {
		return modelMapping{}, fmt.Errorf("model %q is not registered by unified-model", model)
	}
	if strings.TrimSpace(mapping.Target) == "" {
		return modelMapping{}, fmt.Errorf("model %q has no target model configured", model)
	}
	return mapping, nil
}

// hostModelRequestFromExecutor builds the host model execution request for a resolved
// mapping. A pinned credential forces its provider and auth ID so the host selects
// exactly that credential; unpinned mappings leave both empty for native routing.
func hostModelRequestFromExecutor(req rpcExecutorRequest, mapping modelMapping, stream bool) hostModelExecutionRequest {
	return hostModelExecutionRequest{
		HostModelExecutionRequest: pluginapi.HostModelExecutionRequest{
			EntryProtocol:  req.SourceFormat,
			ExitProtocol:   req.Format,
			Model:          mapping.Target,
			Stream:         stream,
			Body:           rewriteRequestBody(req.Payload, mapping.Target, stream),
			Headers:        req.Headers,
			Query:          req.Query,
			Alt:            req.Alt,
			ForcedProvider: mapping.forcedProvider(),
			AuthID:         mapping.pinnedAuthID(),
		},
		HostCallbackID: req.HostCallbackID,
	}
}

func handleManagement(raw []byte) ([]byte, error) {
	var req managementRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return nil, fmt.Errorf("decode management request: %w", errUnmarshal)
		}
	}
	if catalogRequested(req) {
		body, errMarshal := json.Marshal(modelCatalog(mappings.snapshotContextLengths()))
		if errMarshal != nil {
			return nil, errMarshal
		}
		return okEnvelope(managementResponse{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
			Body:       body,
		})
	}
	return okEnvelope(managementResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       managementPage,
	})
}

// catalogRequested reports whether the request asks for the built-in model catalog JSON.
func catalogRequested(req managementRequest) bool {
	if req.Method != "" && !strings.EqualFold(strings.TrimSpace(req.Method), http.MethodGet) {
		return false
	}
	for _, value := range req.Query[catalogQueryParam] {
		if strings.EqualFold(strings.TrimSpace(value), catalogQueryParamValue) {
			return true
		}
	}
	return false
}

func readHostStream(streamID string) (pluginapi.HostModelStreamReadResponse, error) {
	result, errCall := callHost(pluginabi.MethodHostModelStreamRead, pluginapi.HostModelStreamReadRequest{StreamID: streamID})
	if errCall != nil {
		return pluginapi.HostModelStreamReadResponse{}, errCall
	}
	var response pluginapi.HostModelStreamReadResponse
	if errUnmarshal := json.Unmarshal(result, &response); errUnmarshal != nil {
		return pluginapi.HostModelStreamReadResponse{}, fmt.Errorf("decode host stream chunk: %w", errUnmarshal)
	}
	return response, nil
}

func closeHostStream(streamID string) error {
	_, errCall := callHost(pluginabi.MethodHostModelStreamClose, pluginapi.HostModelStreamCloseRequest{StreamID: streamID})
	return errCall
}

func forwardHostStream(hostStreamID, pluginStreamID string) {
	var streamError string
	defer func() {
		if recovered := recover(); recovered != nil {
			streamError = fmt.Sprintf("stream forwarding panic: %v", recovered)
		}
		_ = closeHostStream(hostStreamID)
		_, _ = callHost(pluginabi.MethodHostStreamClose, rpcStreamCloseRequest{
			StreamID: pluginStreamID,
			Error:    streamError,
		})
	}()

	for {
		chunk, errRead := readHostStream(hostStreamID)
		if errRead != nil {
			streamError = errRead.Error()
			return
		}
		if chunk.Error != "" {
			streamError = chunk.Error
			return
		}
		if len(chunk.Payload) > 0 {
			if _, errEmit := callHost(pluginabi.MethodHostStreamEmit, rpcStreamEmitRequest{
				StreamID: pluginStreamID,
				Payload:  append([]byte(nil), chunk.Payload...),
			}); errEmit != nil {
				streamError = errEmit.Error()
				return
			}
		}
		if chunk.Done {
			return
		}
	}
}

func callHost(method string, payload any) (json.RawMessage, error) {
	rawPayload, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return nil, fmt.Errorf("marshal host callback payload %s: %w", method, errMarshal)
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))

	var response C.cliproxy_buffer
	var requestPtr *C.uint8_t
	if len(rawPayload) > 0 {
		cPayload := C.CBytes(rawPayload)
		if cPayload == nil {
			return nil, fmt.Errorf("allocate host callback payload %s", method)
		}
		defer C.free(cPayload)
		requestPtr = (*C.uint8_t)(cPayload)
	}
	callCode := C.call_host_api(cMethod, requestPtr, C.size_t(len(rawPayload)), &response)
	var rawResponse []byte
	if response.ptr != nil && response.len > 0 {
		rawResponse = C.GoBytes(response.ptr, C.int(response.len))
	}
	if response.ptr != nil {
		C.free_host_buffer(response.ptr, response.len)
	}
	if len(rawResponse) == 0 {
		return nil, fmt.Errorf("host callback %s returned no response, code=%d", method, int(callCode))
	}

	var env pluginabi.Envelope
	if errUnmarshal := json.Unmarshal(rawResponse, &env); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host callback envelope %s: %w", method, errUnmarshal)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, fmt.Errorf("%s: %s", env.Error.Code, env.Error.Message)
		}
		return nil, fmt.Errorf("host callback %s failed", method)
	}
	if callCode != 0 {
		return nil, fmt.Errorf("host callback %s returned code=%d", method, int(callCode))
	}
	return append(json.RawMessage(nil), env.Result...), nil
}

func okEnvelope(value any) ([]byte, error) {
	raw, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}
