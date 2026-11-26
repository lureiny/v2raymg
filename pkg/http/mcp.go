package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/buildinfo"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpCallerContextKey struct{}

func withMCPCaller(ctx context.Context, caller Caller) context.Context {
	return context.WithValue(ctx, mcpCallerContextKey{}, caller)
}

func mcpCallerFromContext(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(mcpCallerContextKey{}).(Caller)
	return caller, ok
}

// mcpToolRegistrar captures a type-erased MCP tool registration closure for an
// Endpoint so RegisterMCP can assemble role-scoped MCP servers on demand.
type mcpToolRegistrar struct {
	name     string
	routeKey string
	scope    AuthScope
	readOnly bool
	register func(srv *mcp.Server)
}

func newMCPToolRegistrar[Req any, Resp any](ep Endpoint[Req, Resp]) mcpToolRegistrar {
	return mcpToolRegistrar{
		name:     ep.MCPName,
		scope:    ep.Scope,
		readOnly: ep.ReadOnly,
		register: func(srv *mcp.Server) {
			destructive := ep.Destructive
			tool := &mcp.Tool{
				Name:        ep.MCPName,
				Description: ep.MCPDescription,
				Annotations: &mcp.ToolAnnotations{
					ReadOnlyHint:    ep.ReadOnly,
					DestructiveHint: &destructive,
				},
			}
			mcp.AddTool(srv, tool, func(ctx context.Context, callReq *mcp.CallToolRequest, req Req) (*mcp.CallToolResult, any, error) {
				if ep.Execute == nil {
					return nil, nil, NewEndpointErr(http.StatusInternalServerError, "endpoint handler not configured")
				}
				caller, ok := mcpCallerFromContext(ctx)
				if !ok {
					return nil, nil, NewEndpointErr(http.StatusUnauthorized, "unauthorized")
				}
				// Defense-in-depth scope enforcement in addition to separate
				// adminServer / userServer tool registries.
				if ep.Scope == ScopeAdmin && !caller.IsAdmin() {
					return nil, nil, NewEndpointErr(http.StatusForbidden, "admin role required")
				}

				var rawArgs json.RawMessage
				if callReq != nil && callReq.Params != nil {
					rawArgs = callReq.Params.Arguments
				}
				if err := applyMCPDefaults(rawArgs, &req); err != nil {
					return nil, nil, NewEndpointErr(http.StatusBadRequest, fmt.Sprintf("invalid default value: %v", err))
				}

				resp, err := ep.Execute(ctx, caller, req)
				if err != nil {
					return nil, nil, err
				}

				var out any = resp
				if ep.MCPSanitize != nil {
					out = ep.MCPSanitize(resp)
				}

				data, err := json.Marshal(out)
				if err != nil {
					return nil, nil, fmt.Errorf("marshal mcp tool response: %w", err)
				}

				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: string(data)},
					},
				}, nil, nil
			})
		},
	}
}

// applyMCPDefaults populates struct fields tagged with `default:"<val>"` on req
// when the corresponding JSON argument key is absent (or JSON null, or "" for
// string fields) in rawArgs, while preserving explicit false / 0 / non-empty
// values supplied by the MCP caller.
func applyMCPDefaults[Req any](rawArgs json.RawMessage, req *Req) error {
	rv := reflect.ValueOf(req).Elem()
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			if rv.Type().Elem().Kind() != reflect.Struct {
				return nil
			}
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}

	provided := make(map[string]json.RawMessage)
	trimmed := bytes.TrimSpace(rawArgs)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		_ = json.Unmarshal(trimmed, &provided)
	}

	return applyStructDefaults(rv, provided)
}

func applyStructDefaults(structVal reflect.Value, provided map[string]json.RawMessage) error {
	t := structVal.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		fv := structVal.Field(i)
		if sf.Anonymous {
			if fv.Kind() == reflect.Struct {
				if err := applyStructDefaults(fv, provided); err != nil {
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
				if err := applyStructDefaults(fv.Elem(), provided); err != nil {
					return err
				}
				continue
			}
		}
		if !fv.CanSet() {
			continue
		}

		defaultVal, hasDefault := sf.Tag.Lookup("default")
		if !hasDefault || defaultVal == "" {
			continue
		}

		jsonKey, omit := jsonFieldKey(sf)
		if omit {
			continue
		}

		rawVal, exists := lookupJSONKey(provided, jsonKey)
		isNull := exists && bytes.Equal(bytes.TrimSpace(rawVal), []byte("null"))

		shouldApply := !exists || isNull
		if !shouldApply && fv.Kind() == reflect.String && fv.String() == "" {
			shouldApply = true
		}
		if !shouldApply {
			continue
		}

		if fv.Kind() == reflect.Slice && fv.Type().Elem().Kind() == reflect.String {
			fv.Set(reflect.ValueOf(strings.Split(defaultVal, ",")))
			continue
		}
		if err := setFieldFromString(fv, defaultVal); err != nil {
			return fmt.Errorf("field %s default %q: %w", sf.Name, defaultVal, err)
		}
	}
	return nil
}

func jsonFieldKey(sf reflect.StructField) (string, bool) {
	tag := sf.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	if tag == "" {
		return sf.Name, false
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return sf.Name, false
	}
	return name, false
}

func lookupJSONKey(provided map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	if v, ok := provided[key]; ok {
		return v, true
	}
	for k, v := range provided {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

// RegisterMCP builds role-scoped MCP servers from endpoints registered on s
// and mounts the Stateless Streamable HTTP MCP handler at /mcp protected by
// AuthMiddleware (accepting X-Token as admin or Bearer JWT as user/admin).
//
// Like RegisterPrometheus, /mcp is mounted only when explicitly called.
func RegisterMCP(s *HttpServer) {
	if s == nil || s.RestfulServer == nil {
		return
	}

	impl := &mcp.Implementation{
		Name:    "v2raymg",
		Version: buildinfo.Version,
	}
	serverOpts := &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{},
		},
	}

	adminServer := mcp.NewServer(impl, serverOpts)
	userServer := mcp.NewServer(impl, serverOpts)

	adminTools := 0
	userTools := 0
	for _, reg := range s.mcpRegistrars {
		if reg.name == "" {
			continue
		}
		if s.MCPReadOnly && !reg.readOnly {
			continue
		}
		switch reg.scope {
		case ScopeAdmin:
			reg.register(adminServer)
			adminTools++
		case ScopeUser:
			reg.register(adminServer)
			adminTools++
			reg.register(userServer)
			userTools++
		}
	}

	getServer := func(r *http.Request) *mcp.Server {
		caller, ok := mcpCallerFromContext(r.Context())
		if !ok {
			return nil
		}
		if caller.IsAdmin() {
			return adminServer
		}
		return userServer
	}

	streamableHandler := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})

	mcpAuth := func(c *gin.Context) {
		auth.AuthMiddleware(s.token, s.jwtSecret)(c)
	}
	serveMCP := func(c *gin.Context) {
		caller := callerFromGin(c)
		req := c.Request.WithContext(withMCPCaller(c.Request.Context(), caller))
		streamableHandler.ServeHTTP(c.Writer, req)
	}

	s.RestfulServer.POST("/mcp", mcpAuth, serveMCP)
	s.RestfulServer.GET("/mcp", mcpAuth, serveMCP)
	s.RestfulServer.DELETE("/mcp", mcpAuth, serveMCP)

	log.Info("mcp server registered",
		"read_only", s.MCPReadOnly,
		"admin_tools", adminTools,
		"user_tools", userTools,
	)
}
