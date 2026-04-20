package server

import (
	context "context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// getXrayRuntimeAPI gets the xray container's RuntimeAPI.
func (s *EndNodeServer) getXrayRuntimeAPI() (container.RuntimeAPI, error) {
	c, ok := s.containerMgr.Get(contracts.ContainerXray)
	if !ok {
		return nil, fmt.Errorf("xray container not found")
	}
	rapi, ok := c.(container.RuntimeAPI)
	if !ok {
		return nil, fmt.Errorf("xray container does not implement RuntimeAPI")
	}
	return rapi, nil
}

// getXrayContainer gets the xray Container.
func (s *EndNodeServer) getXrayContainer() (container.Container, error) {
	c, ok := s.containerMgr.Get(contracts.ContainerXray)
	if !ok {
		return nil, fmt.Errorf("xray container not found")
	}
	return c, nil
}

// getSnellContainer gets the snell Container.
func (s *EndNodeServer) getSnellContainer() (container.Container, error) {
	c, ok := s.containerMgr.Get(contracts.ContainerSnell)
	if !ok {
		return nil, fmt.Errorf("snell container not found")
	}
	return c, nil
}

// getContainerByType returns the container for the given type string.
// Empty or "xray" returns the xray container; "snell" returns snell.
func (s *EndNodeServer) getContainerByType(containerType string) (container.Container, error) {
	switch containerType {
	case "", "xray":
		return s.getXrayContainer()
	case "snell":
		return s.getSnellContainer()
	default:
		return nil, fmt.Errorf("unsupported container type: %s", containerType)
	}
}

func (s *EndNodeServer) AddInbound(ctx context.Context, inboundOpReq *proto.InboundOpReq) (*proto.InboundOpRsp, error) {
	inboundOpRsp := &proto.InboundOpRsp{Code: 0}
	rapi, err := s.getXrayRuntimeAPI()
	if err != nil {
		errMsg := fmt.Sprintf("get xray runtime api err > %v", err)
		log.Error("add inbound failed", "err", errMsg)
		inboundOpRsp.Code = 600
		inboundOpRsp.Msg = errMsg
		return inboundOpRsp, nil
	}
	// InboundInfo is base64-encoded xray native JSON (compatible with old proxy/manager/Inbound.Init)
	inboundJSON, decErr := base64.StdEncoding.DecodeString(inboundOpReq.GetInboundInfo())
	if decErr != nil {
		// fallback: treat as raw JSON if not base64
		inboundJSON = []byte(inboundOpReq.GetInboundInfo())
	}
	err = rapi.AddInboundNative(inboundJSON)
	if err != nil {
		errMsg := fmt.Sprintf("add inbound err > %v", err)
		log.Error("add inbound failed", "err", errMsg, "inbound_info", inboundOpReq.GetInboundInfo())
		inboundOpRsp.Msg = errMsg
		inboundOpRsp.Code = 601
		return inboundOpRsp, nil
	}
	return inboundOpRsp, nil
}


func (s *EndNodeServer) GetInbound(ctx context.Context, getInboundReq *proto.GetInboundReq) (*proto.GetInboundRsp, error) {
	getInboundRsp := &proto.GetInboundRsp{Code: 0}
	c, err := s.getXrayContainer()
	if err != nil {
		errMsg := fmt.Sprintf("get xray container err > %v", err)
		log.Error("get inbound failed", "err", errMsg, "tag", getInboundReq.GetTag())
		getInboundRsp.Code = 901
		getInboundRsp.Msg = errMsg
		return getInboundRsp, nil
	}
	inb, err := c.GetInboundConfig(getInboundReq.GetTag())
	if err != nil {
		errMsg := fmt.Sprintf("inbound with tag(%s) is not exist", getInboundReq.GetTag())
		log.Error("get inbound failed", "err", errMsg, "tag", getInboundReq.GetTag())
		getInboundRsp.Code = 901
		getInboundRsp.Msg = errMsg
		return getInboundRsp, nil
	}
	data, err := inb.ToNative()
	if err != nil {
		log.Error("encode inbound failed", "err", err.Error(), "tag", getInboundReq.GetTag())
		getInboundRsp.Code = 902
		getInboundRsp.Msg = err.Error()
		return getInboundRsp, nil
	}
	getInboundRsp.Data = string(data)
	return getInboundRsp, nil
}

func (s *EndNodeServer) UpdateProxy(ctx context.Context, updateProxyReq *proto.UpdateProxyReq) (*proto.UpdateProxyRsp, error) {
	updateProxyRsp := &proto.UpdateProxyRsp{Code: 0}
	tag := updateProxyReq.GetTag()
	c, err := s.getXrayContainer()
	if err != nil {
		log.Error("update proxy failed", "err", err.Error())
		updateProxyRsp.Code = 1002
		updateProxyRsp.Msg = err.Error()
		return updateProxyRsp, nil
	}
	currentVersion := c.Version()
	if tag == currentVersion || "v"+tag == currentVersion {
		updateProxyRsp.Msg = "current version is same with dst version"
		return updateProxyRsp, nil
	}
	result, err := c.Update(ctx, container.UpdateRequest{TargetTag: tag})
	if err != nil {
		log.Error("update proxy failed", "err", err.Error(), "tag", tag)
		updateProxyRsp.Code = 1002
		updateProxyRsp.Msg = err.Error()
		return updateProxyRsp, nil
	}
	if result != nil {
		log.Info("proxy updated", "to_version", result.ToVersion)
	}
	return updateProxyRsp, nil
}

func (s *EndNodeServer) SetGatewayModel(ctx context.Context, setGatewayModelReq *proto.SetGatewayModelReq) (*proto.SetGatewayModelRsp, error) {
	setGatewayModelRsp := &proto.SetGatewayModelRsp{Code: 0}
	c, err := s.getXrayContainer()
	if err != nil {
		log.Error("set gateway model failed", "err", err.Error())
		setGatewayModelRsp.Code = 1
		setGatewayModelRsp.Msg = err.Error()
		return setGatewayModelRsp, nil
	}
	if setGatewayModelReq.GetEnableGatewayModel() {
		_ = c.Stop()
	} else {
		_ = c.Start()
	}
	s.cfg.OnlyGateway = setGatewayModelReq.GetEnableGatewayModel()
	return setGatewayModelRsp, nil
}

func (s *EndNodeServer) FastAddInbound(ctx context.Context, fastAddInboundReq *proto.FastAddInboundReq) (*proto.FastAddInboundRsp, error) {
	fastAddInboundRsp := &proto.FastAddInboundRsp{Code: 0}

	c, err := s.getContainerByType(fastAddInboundReq.GetContainerType())
	if err != nil {
		fastAddInboundRsp.Code = 1020
		fastAddInboundRsp.Msg = err.Error()
		return fastAddInboundRsp, nil
	}

	var protocol string
	switch fastAddInboundReq.GetInboundBuilderType() {
	case proto.BuilderType_VLESSSettingBuilderType:
		protocol = "vless"
	case proto.BuilderType_VMESSSettingBuilderType:
		protocol = "vmess"
	case proto.BuilderType_TrojanSettingBuilderType:
		protocol = "trojan"
	case proto.BuilderType_SSSettingBuilderType:
		protocol = "shadowsocks"
	default:
		fastAddInboundRsp.Code = 1020
		fastAddInboundRsp.Msg = "unsupport protocol"
		return fastAddInboundRsp, nil
	}

	// Resolve transport: prefer new string field, fall back to legacy enum
	transport := fastAddInboundReq.GetTransport()
	if transport == "" {
		switch fastAddInboundReq.GetStreamBuilderType() {
		case proto.BuilderType_TCPBuilderType:
			transport = "tcp"
		case proto.BuilderType_WSBuilderType:
			transport = "ws"
		case proto.BuilderType_GrpcBuilderType:
			transport = "grpc"
		case proto.BuilderType_HttpBuilderType:
			transport = "h2"
		default:
			transport = "tcp"
		}
	}

	security := "tls"
	if fastAddInboundReq.GetSecurity() != "" {
		security = fastAddInboundReq.GetSecurity()
	} else if fastAddInboundReq.GetIsXtls() {
		security = "xtls"
	}

	// Cert acquisition: only when domain is provided AND security actually needs a cert (tls/xtls).
	// Reality, none, and other security types don't use domain certs.
	domain := fastAddInboundReq.GetDomain()
	if domain != "" && (security == "tls" || security == "xtls") {
		if cert := s.certManager.GetCertInfo(domain); cert == nil {
			if err := s.certManager.ObtainNewCert(domain); err != nil {
				fastAddInboundRsp.Code = 1022
				fastAddInboundRsp.Msg = fmt.Sprintf("obtain new cert of domain[%s] fail > %v", domain, err)
				return fastAddInboundRsp, nil
			}
		}
	}

	params := map[string]any{
		"protocol":  protocol,
		"port":      fastAddInboundReq.GetPort(),
		"domain":    domain,
		"security":  security,
		"transport": transport,
		"tag":       fastAddInboundReq.GetTag(),
	}
	// If no domain is provided and security requires TLS, use self-signed cert
	if fastAddInboundReq.GetDomain() == "" && (security == "tls" || security == "xtls") {
		params["self_signed"] = true
	}
	if fastAddInboundReq.GetRealityTarget() != "" {
		params["reality_target"] = fastAddInboundReq.GetRealityTarget()
	}
	if len(fastAddInboundReq.GetRealityServerNames()) > 0 {
		params["reality_server_names"] = fastAddInboundReq.GetRealityServerNames()
	}
	if len(fastAddInboundReq.GetRealityShortIds()) > 0 {
		params["reality_short_ids"] = fastAddInboundReq.GetRealityShortIds()
	}

	// Merge extra_params: pass through all transport/security/sniffing params to Executor.
	// String values are passed as-is; special keys are converted to appropriate types.
	for k, v := range fastAddInboundReq.GetExtraParams() {
		switch k {
		case "sniffing_enabled", "sniffing_route_only", "tls_reject_unknown_sni", "self_signed":
			params[k] = v == "true"
		case "ocsp_stapling":
			var n int
			fmt.Sscanf(v, "%d", &n)
			params[k] = n
		case "reality_server_names", "sniffing_dest_override", "alpn", "xhttp_host", "http_host":
			params[k] = strings.Split(v, ",")
		case "reality_short_ids":
			if _, exists := params["reality_short_ids"]; !exists {
				params[k] = strings.Split(v, ",")
			}
		default:
			// String params: ws_path, grpc_service_name, httpupgrade_path, httpupgrade_host,
			// xhttp_path, xhttp_mode, tls_min_version, flow, uuid, method, password, etc.
			params[k] = v
		}
	}

	if err := c.FastAddInbound(fastAddInboundReq.GetTag(), params); err != nil {
		fastAddInboundRsp.Code = 1021
		fastAddInboundRsp.Msg = err.Error()
	}
	return fastAddInboundRsp, nil
}

// ListInbound returns all inbounds across all registered containers.
// Each InboundInfo contains the container type and the inbound's unique name (tag).
func (s *EndNodeServer) ListInbound(ctx context.Context, req *proto.ListInboundReq) (*proto.ListInboundRsp, error) {
	rsp := &proto.ListInboundRsp{Code: 0}

	containerTypes := []contracts.ContainerType{
		contracts.ContainerXray,
		contracts.ContainerSnell,
		contracts.ContainerHysteria,
	}

	for _, ct := range containerTypes {
		c, ok := s.containerMgr.Get(ct)
		if !ok {
			continue
		}
		for _, inb := range c.ListInboundConfigs() {
			rsp.Inbounds = append(rsp.Inbounds, &proto.InboundInfo{
				Container: string(ct),
				Name:      inb.Tag(),
			})
		}
	}

	return rsp, nil
}

// DeleteInboundByName deletes an inbound identified by container type and name (tag).
// Supports xray, snell, and hysteria containers.
func (s *EndNodeServer) DeleteInboundByName(ctx context.Context, req *proto.DeleteInboundByNameReq) (*proto.InboundOpRsp, error) {
	rsp := &proto.InboundOpRsp{Code: 0}

	ct := contracts.ContainerType(req.GetContainer())
	name := req.GetName()

	if name == "" {
		rsp.Code = 601
		rsp.Msg = "name is required"
		return rsp, nil
	}

	c, ok := s.containerMgr.Get(ct)
	if !ok {
		rsp.Code = 601
		rsp.Msg = fmt.Sprintf("container %q not found", req.GetContainer())
		return rsp, nil
	}

	if err := c.RemoveInboundConfig(name); err != nil {
		errMsg := fmt.Sprintf("delete inbound failed: %v", err)
		log.Error("DeleteInboundByName failed", "container", req.GetContainer(), "name", name, "err", err)
		rsp.Code = 601
		rsp.Msg = errMsg
		return rsp, nil
	}

	log.Info("DeleteInboundByName success", "container", req.GetContainer(), "name", name)
	return rsp, nil
}
