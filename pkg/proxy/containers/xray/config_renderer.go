// Package xray provides Xray config rendering.
package xray

import (
	"encoding/json"
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// APIInboundTag is the tag for the API inbound.
const APIInboundTag = "api"

// Renderer renders Xray configuration.
type Renderer struct {
	adapter *Adapter
}

// NewRenderer creates a new Xray renderer.
func NewRenderer() *Renderer {
	return &Renderer{
		adapter: NewAdapter(),
	}
}

// ToProvider renders domain ContainerModel to Xray config.
func (r *Renderer) ToProvider(model contracts.ContainerModel) (NativeConfig, error) {
	config := map[string]interface{}{
		"log": map[string]string{
			"loglevel": "warning",
		},
		"stats":    map[string]interface{}{},
		"policy":   r.buildPolicy(),
		"api":      r.buildAPI(model.APIPort),
		"inbounds": []interface{}{},
		"outbounds": []interface{}{
			r.buildDirectOutbound(),
		},
		"routing": r.buildRouting(),
	}

	// Render inbounds from model
	inbounds, err := r.renderInbounds(model)
	if err != nil {
		return NativeConfig{}, err
	}
	config["inbounds"] = inbounds

	// Add user bindings to inbounds
	if len(model.UserBindings) > 0 {
		config["inbounds"], err = r.applyUserBindings(config["inbounds"].([]interface{}), model.UserBindings)
		if err != nil {
			return NativeConfig{}, err
		}
	}

	data, err := json.Marshal(config)
	if err != nil {
		return NativeConfig{}, err
	}

	return NativeConfig{JSON: data}, nil
}

// renderInbounds renders inbounds from the model.
func (r *Renderer) renderInbounds(model contracts.ContainerModel) ([]interface{}, error) {
	inbounds := make([]interface{}, 0, len(model.Inbounds)+1) // +1 for API inbound

	// Add API inbound first
	apiInbound := r.buildAPIInbound(model.APIPort)
	inbounds = append(inbounds, apiInbound)

	// Add user inbounds
	for _, in := range model.Inbounds {
		native, err := r.adapter.ToProvider(in)
		if err != nil {
			return nil, fmt.Errorf("failed to render inbound %s: %w", in.Tag, err)
		}

		var m map[string]interface{}
		if err := json.Unmarshal(native.JSON, &m); err != nil {
			return nil, fmt.Errorf("failed to unmarshal inbound %s: %w", in.Tag, err)
		}
		inbounds = append(inbounds, m)
	}

	return inbounds, nil
}

// applyUserBindings adds users to inbound settings.
func (r *Renderer) applyUserBindings(inbounds []interface{}, userBindings map[string][]contracts.UserSpec) ([]interface{}, error) {
	for i, in := range inbounds {
		inMap, ok := in.(map[string]interface{})
		if !ok {
			continue
		}

		tag, ok := inMap["tag"].(string)
		if !ok {
			continue
		}

		users, hasUsers := userBindings[tag]
		if !hasUsers || tag == APIInboundTag {
			continue
		}

		// Get protocol from inbound
		protocolStr, ok := inMap["protocol"].(string)
		if !ok {
			continue
		}
		protocol := contracts.Protocol(protocolStr)

		// Build users for this protocol
		userMaps, err := r.adapter.MapUsers(users, protocol)
		if err != nil {
			return nil, fmt.Errorf("failed to map users for inbound %s: %w", tag, err)
		}

		// Get or create settings
		settings, ok := inMap["settings"].(map[string]interface{})
		if !ok {
			settings = map[string]interface{}{}
			inMap["settings"] = settings
		}

		// Add users based on protocol
		switch protocol {
		case contracts.ProtocolVMess, contracts.ProtocolVLess, contracts.ProtocolTrojan:
			// Convert []map[string]interface{} to []interface{} for proper JSON serialization
			clientSlice := make([]interface{}, len(userMaps))
			for i, m := range userMaps {
				clientSlice[i] = m
			}
			settings["clients"] = clientSlice
		case contracts.ProtocolShadowsocks:
			if len(users) > 0 {
				if uuid, ok := users[0].GetExtension("uuid"); ok {
					if uuidStr, ok := uuid.(string); ok {
						settings["password"] = uuidStr
					}
				}
			}
		}

		inbounds[i] = inMap
	}

	return inbounds, nil
}

// buildAPI builds the API configuration (top-level, only tag and services).
func (r *Renderer) buildAPI(port int) map[string]interface{} {
	return map[string]interface{}{
		"tag":      APIInboundTag,
		"services": []string{"HandlerService", "LoggerService", "StatsService"},
	}
}

// buildAPIInbound builds the API inbound configuration.
func (r *Renderer) buildAPIInbound(port int) map[string]interface{} {
	return map[string]interface{}{
		"tag":      APIInboundTag,
		"port":     port,
		"listen":   "127.0.0.1",
		"protocol": "dokodemo-door",
		"settings": map[string]interface{}{
			"address": "127.0.0.1",
		},
	}
}

// buildPolicy builds the policy configuration.
func (r *Renderer) buildPolicy() map[string]interface{} {
	return map[string]interface{}{
		"levels": map[string]interface{}{
			"0": map[string]interface{}{
				"statsUserUplink":       true,
				"statsUserDownlink":     true,
				"statsInboundUplink":    true,
				"statsInboundDownlink":  true,
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		},
		"system": map[string]interface{}{
			"statsInboundUplink":    true,
			"statsInboundDownlink":  true,
			"statsOutboundUplink":   true,
			"statsOutboundDownlink": true,
		},
	}
}

// buildDirectOutbound builds the direct outbound configuration.
func (r *Renderer) buildDirectOutbound() map[string]interface{} {
	return map[string]interface{}{
		"tag":      "direct",
		"protocol": "freedom",
		"settings": map[string]interface{}{
			"domainStrategy": "AsIs",
		},
	}
}

// buildRouting builds the routing configuration.
func (r *Renderer) buildRouting() map[string]interface{} {
	return map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{
				"inboundTag":  []string{APIInboundTag},
				"outboundTag": "api",
			},
		},
	}
}

// FromProvider parses Xray config to domain model.
func (r *Renderer) FromProvider(native NativeConfig) (contracts.ContainerModel, UnmappedWarnings, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		return contracts.ContainerModel{}, nil, err
	}

	model := contracts.ContainerModel{
		Type:     contracts.ContainerXray,
		Inbounds: []contracts.InboundSpec{},
	}

	warnings := UnmappedWarnings{}

	// Parse inbounds
	if inbounds, ok := m["inbounds"].([]interface{}); ok {
		for _, in := range inbounds {
			inMap, ok := in.(map[string]interface{})
			if !ok {
				continue
			}

			tag, _ := inMap["tag"].(string)
			// Skip API inbound
			if tag == APIInboundTag {
				continue
			}

			inboundJSON, err := json.Marshal(inMap)
			if err != nil {
				warnings = append(warnings, UnmappedWarning{
					FieldPath: "inbounds",
					Reason:    err.Error(),
					Provider:  "xray",
				})
				continue
			}

			spec, ibWarnings, err := r.adapter.FromProvider(NativeInbound{JSON: inboundJSON})
			if err != nil {
				warnings = append(warnings, UnmappedWarning{
					FieldPath: "inbounds",
					Reason:    err.Error(),
					Provider:  "xray",
				})
				continue
			}
			warnings = append(warnings, ibWarnings...)

			model.Inbounds = append(model.Inbounds, spec)
		}
	}

	// Check for unmapped top-level fields
	for k := range m {
		if k != "log" && k != "stats" && k != "policy" && k != "api" &&
			k != "inbounds" && k != "outbounds" && k != "routing" {
			warnings = append(warnings, UnmappedWarning{
				FieldPath: k,
				Reason:    "unmapped top-level field",
				Provider:  "xray",
			})
		}
	}

	return model, warnings, nil
}
