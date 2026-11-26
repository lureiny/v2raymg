package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
)

// AuthScope defines the authorization scope required by an Endpoint.
type AuthScope int

const (
	ScopePublic AuthScope = iota
	ScopeUser
	ScopeAdmin
)

// Caller represents the authenticated caller extracted from HTTP middleware
// or propagated through MCP request context.
type Caller struct {
	Username string
	Role     string
}

// IsAdmin reports whether the caller holds the admin role.
func (c Caller) IsAdmin() bool {
	return c.Role == "admin"
}

func callerFromGin(c *gin.Context) Caller {
	var caller Caller
	if v, ok := c.Get(auth.ContextKeyUsername); ok {
		caller.Username, _ = v.(string)
	}
	if v, ok := c.Get(auth.ContextKeyRole); ok {
		caller.Role, _ = v.(string)
	}
	return caller
}

// EndpointError carries both an HTTP status code and a JSON business code so
// HTTP callers keep legacy response semantics (including HTTP 200 with
// code=300/404) while MCP callers uniformly observe an error with IsError=true.
type EndpointError struct {
	HTTPStatus int
	Code       int
	Msg        string
}

func (e *EndpointError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("endpoint error (status=%d, code=%d)", e.HTTPStatus, e.Code)
}

// NewEndpointErr constructs an EndpointError whose business Code equals status.
func NewEndpointErr(status int, msg string) *EndpointError {
	return &EndpointError{
		HTTPStatus: status,
		Code:       status,
		Msg:        msg,
	}
}

// NewBusinessErr constructs an EndpointError with distinct HTTP status and
// business error code (e.g. legacy HTTP 200 with code=300 or code=404).
func NewBusinessErr(httpStatus, code int, msg string) *EndpointError {
	return &EndpointError{
		HTTPStatus: httpStatus,
		Code:       code,
		Msg:        msg,
	}
}

// ActionFunc is the shared transport-agnostic execution function invoked by
// both HTTP (Gin) handlers and embedded MCP tool handlers.
type ActionFunc[Req any, Resp any] func(ctx context.Context, caller Caller, req Req) (Resp, error)

// Endpoint describes a single HTTP API endpoint that can optionally also be
// exposed as an MCP tool without duplicating business logic.
//
// Request struct tag conventions for dual HTTP + MCP binding:
//   - json:"field,omitempty" — JSON body & MCP tool input schema property.
//     Always include ",omitempty" on optional fields so jsonschema-go does not
//     mark them required for MCP callers.
//   - jsonschema:"description" — human-readable MCP schema description
//     (do not prefix with "description=").
//   - form:"field" — form field name for application/x-www-form-urlencoded.
//   - query:"field" — URL query parameter name (supports string, bool, int*,
//     uint*, *int*, *uint*, *bool, *string, []string).
//   - default:"val" — optional default value applied on both HTTP (when a
//     query parameter is absent or empty and not already populated from the
//     request body) and MCP (when the JSON argument key is absent, null, or an
//     empty string for string fields; explicit false/0 values are preserved).
//   - path:"field" — Gin URL path parameter name; bound last so path params
//     always override any body or query field of the same name.
type Endpoint[Req any, Resp any] struct {
	Method         string // "GET", "POST", "PUT", "DELETE"
	Path           string // relative path like "/status", "/node/:name/groups"
	Scope          AuthScope
	Help           string
	OptionalBody   bool   // allows empty body on POST/PUT/DELETE without 400
	MCPName        string // empty = do not expose to MCP
	MCPDescription string
	ReadOnly       bool // maps to MCP ToolAnnotations ReadOnlyHint
	Destructive    bool // maps to MCP ToolAnnotations DestructiveHint
	Execute        ActionFunc[Req, Resp]
	MCPSanitize    func(resp Resp) any // optional; transforms response ONLY for MCP callers
}

// bindEndpointReq binds an HTTP request into Req in three ordered phases:
//  1. Body (POST/PUT/DELETE): form binding for form content types, JSON otherwise.
//     Empty body is allowed when optionalBody is true; malformed JSON returns 400.
//     Pointer fields (*int, *int64, etc.) preserve nil vs explicit 0 semantics.
//  2. Query params: struct fields tagged with query:"<name>" (and optional default:"<val>").
//  3. Path params: struct fields tagged with path:"<name>" (always applied last so
//     path params override any body or query field of the same name).
func bindEndpointReq[Req any](c *gin.Context, method string, optionalBody bool) (Req, error) {
	var req Req

	// If Req is a pointer to struct, allocate it before binding.
	rv := reflect.ValueOf(&req).Elem()
	if rv.Kind() == reflect.Ptr && rv.Type().Elem().Kind() == reflect.Struct && rv.IsNil() {
		rv.Set(reflect.New(rv.Type().Elem()))
	}

	upperMethod := strings.ToUpper(method)
	if upperMethod == http.MethodPost || upperMethod == http.MethodPut || upperMethod == http.MethodDelete {
		if err := bindRequestBody(c, &req, optionalBody); err != nil {
			return req, err
		}
	}

	// Unwrap pointer if Req is *T for struct-tag binding.
	targetVal := rv
	if targetVal.Kind() == reflect.Ptr {
		targetVal = targetVal.Elem()
	}
	if targetVal.Kind() == reflect.Struct {
		if err := bindQueryTags(c, targetVal); err != nil {
			return req, err
		}
		if err := bindPathTags(c, targetVal); err != nil {
			return req, err
		}
	}

	return req, nil
}

func bindRequestBody(c *gin.Context, dst any, optionalBody bool) error {
	ct := c.ContentType()
	isForm := ct == gin.MIMEPOSTForm || ct == gin.MIMEMultipartPOSTForm
	if isForm {
		if c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
			if optionalBody {
				return nil
			}
			return NewEndpointErr(http.StatusBadRequest, "invalid request body: missing form body")
		}
		if err := c.ShouldBind(dst); err != nil {
			return NewEndpointErr(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		}
		return nil
	}

	if c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		if optionalBody {
			return nil
		}
		return NewEndpointErr(http.StatusBadRequest, "invalid request body: empty body")
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return NewEndpointErr(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		if optionalBody {
			return nil
		}
		return NewEndpointErr(http.StatusBadRequest, "invalid request body: EOF")
	}

	if err := json.Unmarshal(bodyBytes, dst); err != nil {
		return NewEndpointErr(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}
	return nil
}

func bindQueryTags(c *gin.Context, structVal reflect.Value) error {
	t := structVal.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		fv := structVal.Field(i)
		if sf.Anonymous {
			if fv.Kind() == reflect.Struct {
				if err := bindQueryTags(c, fv); err != nil {
					return err
				}
				continue
			}
			if fv.Kind() == reflect.Ptr && fv.Type().Elem().Kind() == reflect.Struct {
				if fv.IsNil() {
					if !fv.CanSet() {
						continue
					}
					fv.Set(reflect.New(fv.Type().Elem()))
				}
				if err := bindQueryTags(c, fv.Elem()); err != nil {
					return err
				}
				continue
			}
		}
		if !fv.CanSet() {
			continue
		}

		queryName := sf.Tag.Get("query")
		if queryName == "" {
			continue
		}
		defaultVal, hasDefault := sf.Tag.Lookup("default")

		vals, exists := c.GetQueryArray(queryName)

		// []string supports repeated query keys (?k=a&k=b) and comma-separated default.
		if fv.Kind() == reflect.Slice && fv.Type().Elem().Kind() == reflect.String {
			if exists {
				copied := make([]string, len(vals))
				copy(copied, vals)
				fv.Set(reflect.ValueOf(copied))
			} else if hasDefault && defaultVal != "" && fv.IsZero() {
				fv.Set(reflect.ValueOf(strings.Split(defaultVal, ",")))
			}
			continue
		}

		var raw string
		var shouldSet bool
		if exists && len(vals) > 0 && vals[0] != "" {
			raw = vals[0]
			shouldSet = true
		} else if hasDefault && defaultVal != "" && fv.IsZero() {
			raw = defaultVal
			shouldSet = true
		}

		if !shouldSet {
			continue
		}
		if err := setFieldFromString(fv, raw); err != nil {
			return NewEndpointErr(http.StatusBadRequest, fmt.Sprintf("invalid query param %q: %v", queryName, err))
		}
	}
	return nil
}

func bindPathTags(c *gin.Context, structVal reflect.Value) error {
	t := structVal.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		fv := structVal.Field(i)
		if sf.Anonymous {
			if fv.Kind() == reflect.Struct {
				if err := bindPathTags(c, fv); err != nil {
					return err
				}
				continue
			}
			if fv.Kind() == reflect.Ptr && fv.Type().Elem().Kind() == reflect.Struct {
				if fv.IsNil() {
					if !fv.CanSet() {
						continue
					}
					fv.Set(reflect.New(fv.Type().Elem()))
				}
				if err := bindPathTags(c, fv.Elem()); err != nil {
					return err
				}
				continue
			}
		}
		if !fv.CanSet() {
			continue
		}

		pathName := sf.Tag.Get("path")
		if pathName == "" {
			continue
		}
		val, ok := c.Params.Get(pathName)
		if !ok || val == "" {
			continue
		}
		if err := setFieldFromString(fv, val); err != nil {
			return NewEndpointErr(http.StatusBadRequest, fmt.Sprintf("invalid path param %q: %v", pathName, err))
		}
	}
	return nil
}

func setFieldFromString(fv reflect.Value, raw string) error {
	if fv.Kind() == reflect.Ptr {
		elem := reflect.New(fv.Type().Elem())
		if err := setScalarFromString(elem.Elem(), raw); err != nil {
			return err
		}
		fv.Set(elem)
		return nil
	}
	return setScalarFromString(fv, raw)
}

func setScalarFromString(fv reflect.Value, raw string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
		return nil
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(raw, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetUint(u)
		return nil
	default:
		return fmt.Errorf("unsupported field type %s", fv.Type())
	}
}

func writeEndpointError(c *gin.Context, err error) {
	var epErr *EndpointError
	if errors.As(err, &epErr) {
		httpStatus := epErr.HTTPStatus
		if httpStatus == 0 {
			httpStatus = http.StatusInternalServerError
		}
		code := epErr.Code
		if code == 0 {
			code = httpStatus
		}
		c.JSON(httpStatus, gin.H{"code": code, "msg": epErr.Msg})
		return
	}
	jsonErr(c, http.StatusInternalServerError, err.Error())
}

// endpointHandlerFunc turns an Endpoint into a gin.HandlerFunc so existing
// *Handler structs can delegate handlerFunc and keep legacy direct-invocation
// unit tests working without breakage.
func endpointHandlerFunc[Req any, Resp any](ep Endpoint[Req, Resp]) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ep.Execute == nil {
			jsonErr(c, http.StatusInternalServerError, "endpoint handler not configured")
			return
		}
		req, err := bindEndpointReq[Req](c, ep.Method, ep.OptionalBody)
		if err != nil {
			writeEndpointError(c, err)
			return
		}
		caller := callerFromGin(c)
		resp, err := ep.Execute(c.Request.Context(), caller, req)
		if err != nil {
			writeEndpointError(c, err)
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

// endpointHelpAdapter bridges an Endpoint into HttpServer.handlersMap so
// HelpHandler (/help/*) continues to work identically for migrated endpoints.
type endpointHelpAdapter struct {
	HttpHandlerImp
	path     string
	helpText string
	fn       gin.HandlerFunc
}

func (a *endpointHelpAdapter) handlerFunc(c *gin.Context)     { a.fn(c) }
func (a *endpointHelpAdapter) getHandlers() []gin.HandlerFunc { return []gin.HandlerFunc{a.fn} }
func (a *endpointHelpAdapter) getRelativePath() string        { return a.path }
func (a *endpointHelpAdapter) help() string                   { return a.helpText }

// registerEndpoint mounts ep on the given Gin router, records its help entry
// in s.handlersMap[method+" "+ep.Path], and registers an MCP tool registrar
// when ep.MCPName is non-empty.
func registerEndpoint[Req any, Resp any](s *HttpServer, router gin.IRouter, ep Endpoint[Req, Resp]) {
	method := strings.ToUpper(ep.Method)
	h := endpointHandlerFunc(ep)

	if s.handlersMap == nil {
		s.handlersMap = make(map[string]HttpHandlerInterface)
	}
	adapter := &endpointHelpAdapter{
		path:     ep.Path,
		helpText: ep.Help,
		fn:       h,
	}
	adapter.setHttpServer(s)
	s.handlersMap[method+" "+ep.Path] = adapter

	switch method {
	case http.MethodGet:
		router.GET(ep.Path, h)
	case http.MethodPost:
		router.POST(ep.Path, h)
	case http.MethodPut:
		router.PUT(ep.Path, h)
	case http.MethodDelete:
		router.DELETE(ep.Path, h)
	default:
		panic(fmt.Sprintf("registerEndpoint: unsupported HTTP method %q for path %q", ep.Method, ep.Path))
	}

	if ep.MCPName != "" {
		reg := newMCPToolRegistrar(ep)
		reg.routeKey = method + " " + joinRoutePath(router, ep.Path)
		s.mcpRegistrars = append(s.mcpRegistrars, reg)
	}
}

func joinRoutePath(router gin.IRouter, relativePath string) string {
	base := ""
	if bp, ok := router.(interface{ BasePath() string }); ok {
		base = bp.BasePath()
	}
	if base == "" {
		return relativePath
	}
	joined := path.Join(base, relativePath)
	if strings.HasSuffix(relativePath, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}
