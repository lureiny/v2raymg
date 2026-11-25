package server

import (
	context "context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/proxy/core/chain"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params"
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

// getContainerByType looks up a registered container by type string.
// Empty string defaults to xray for backward compatibility with older callers
// that omit container_type. "hysteria2" is normalized to hysteria (legacy alias).
// Any other value is passed through to the ContainerMgr; unknown or unregistered
// types surface as "container not found or not enabled".
func (s *EndNodeServer) getContainerByType(containerType string) (container.Container, error) {
	ct := contracts.ContainerType(containerType)
	switch ct {
	case "":
		ct = contracts.ContainerXray
	case "hysteria2":
		ct = contracts.ContainerHysteria
	}
	c, ok := s.containerMgr.Get(ct)
	if !ok {
		// Use the resolved ct in the message so empty/"hysteria2" input
		// shows the actual lookup that failed, not just the raw caller string.
		return nil, fmt.Errorf("container %q (resolved from %q) not found or not enabled", ct, containerType)
	}
	return c, nil
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
	var nativeHeader struct {
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(inboundJSON, &nativeHeader); err == nil && nativeHeader.Tag != "" {
		if err := contracts.ValidateInboundTag(nativeHeader.Tag); err != nil {
			errMsg := fmt.Sprintf("invalid inbound tag %q: %v", nativeHeader.Tag, err)
			log.Error("add inbound failed", "err", errMsg)
			inboundOpRsp.Msg = errMsg
			inboundOpRsp.Code = 601
			return inboundOpRsp, nil
		}
		if existingCT, exists := s.inboundTagExistsAnywhere(nativeHeader.Tag); exists {
			errMsg := fmt.Sprintf("inbound tag %q already exists in container %s", nativeHeader.Tag, existingCT)
			log.Error("add inbound failed", "err", errMsg)
			inboundOpRsp.Msg = errMsg
			inboundOpRsp.Code = 601
			return inboundOpRsp, nil
		}
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
	case proto.BuilderType_Hysteria2SettingBuilderType:
		protocol = "hysteria2"
	case proto.BuilderType_TUICSettingBuilderType:
		protocol = "tuic"
	case proto.BuilderType_AnyTLSSettingBuilderType:
		protocol = "anytls"
	default:
		fastAddInboundRsp.Code = 1020
		fastAddInboundRsp.Msg = "unsupport protocol"
		return fastAddInboundRsp, nil
	}

	// Resolve transport: prefer new string field, fall back to legacy enum.
	// QUIC-native (hysteria2, tuic) and TCP-native (anytls) have no pluggable
	// transport — leave transport empty so downstream protocolparams can
	// validate correctly instead of rejecting "tcp" as an invalid combination.
	transport := fastAddInboundReq.GetTransport()
	if transport == "" {
		switch protocol {
		case "hysteria2", "tuic", "anytls":
			// protocol-native; no transport layer
		default:
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
	}

	security := "tls"
	if fastAddInboundReq.GetSecurity() != "" {
		security = fastAddInboundReq.GetSecurity()
	} else if fastAddInboundReq.GetIsXtls() {
		security = "xtls"
	}

	rawType := strings.TrimSpace(fastAddInboundReq.GetInboundType())
	chainURI := strings.TrimSpace(fastAddInboundReq.GetChainUri())
	var inboundType contracts.InboundType
	if rawType == "" && chainURI != "" {
		inboundType = contracts.InboundTypeChain
	} else {
		inboundType, err = contracts.NormalizeInboundType(rawType)
		if err != nil {
			fastAddInboundRsp.Code = 1020
			fastAddInboundRsp.Msg = err.Error()
			return fastAddInboundRsp, nil
		}
	}
	if inboundType == contracts.InboundTypeChain && chainURI == "" {
		fastAddInboundRsp.Code = 1020
		fastAddInboundRsp.Msg = "chain inbound requires chain_uri"
		return fastAddInboundRsp, nil
	}
	if inboundType == contracts.InboundTypeDirect && chainURI != "" {
		fastAddInboundRsp.Code = 1020
		fastAddInboundRsp.Msg = "direct inbound must not specify chain_uri"
		return fastAddInboundRsp, nil
	}

	accessTags, err := contracts.NormalizeAccessTags(fastAddInboundReq.GetAccessTags())
	if err != nil {
		fastAddInboundRsp.Code = 1020
		fastAddInboundRsp.Msg = fmt.Sprintf("invalid access_tags: %v", err)
		return fastAddInboundRsp, nil
	}
	remark := strings.TrimSpace(fastAddInboundReq.GetRemark())

	tag := strings.TrimSpace(fastAddInboundReq.GetTag())
	if tag == "" {
		tag, err = generateInboundTag(protocol, inboundType)
		if err != nil {
			fastAddInboundRsp.Code = 1020
			fastAddInboundRsp.Msg = fmt.Sprintf("generate inbound tag failed: %v", err)
			return fastAddInboundRsp, nil
		}
	}
	if err := contracts.ValidateInboundTag(tag); err != nil {
		fastAddInboundRsp.Code = 1020
		fastAddInboundRsp.Msg = fmt.Sprintf("invalid tag %q: %v", tag, err)
		return fastAddInboundRsp, nil
	}
	if existingCT, exists := s.inboundTagExistsAnywhere(tag); exists {
		fastAddInboundRsp.Code = 1020
		fastAddInboundRsp.Msg = fmt.Sprintf("inbound tag %q already exists in container %s", tag, existingCT)
		return fastAddInboundRsp, nil
	}

	if inboundType == contracts.InboundTypeChain {
		if extraListen, ok := fastAddInboundReq.GetExtraParams()["listen"]; ok {
			trimmedListen := strings.TrimSpace(extraListen)
			if trimmedListen != "" && trimmedListen != "127.0.0.1" {
				fastAddInboundRsp.Code = 1020
				fastAddInboundRsp.Msg = fmt.Sprintf("chain inbound listen must be 127.0.0.1, got %q", extraListen)
				return fastAddInboundRsp, nil
			}
		}
		cc, ok := c.(container.ChainCapable)
		if !ok || !cc.SupportsChainOutbound() {
			fastAddInboundRsp.Code = 1020
			fastAddInboundRsp.Msg = fmt.Sprintf("container %s does not support chain outbound", c.Type())
			return fastAddInboundRsp, nil
		}
		spec, err := chain.ParseAndValidate(chainURI, s.buildChainAddressPolicy())
		if err != nil {
			fastAddInboundRsp.Code = 1020
			fastAddInboundRsp.Msg = fmt.Sprintf("invalid chain_uri (%s): %v", chain.RedactURI(chainURI), err)
			return fastAddInboundRsp, nil
		}
		if err := cc.ValidateChainURI(chainURI); err != nil {
			fastAddInboundRsp.Code = 1020
			fastAddInboundRsp.Msg = fmt.Sprintf("container %s rejected chain_uri (%s): %v", c.Type(), chain.RedactURI(chainURI), err)
			return fastAddInboundRsp, nil
		}
		if remark == "" {
			remark = spec.Remark
		}
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

	reqParams := map[string]any{
		"protocol": protocol,
		"port":     fastAddInboundReq.GetPort(),
		"domain":   domain,
		"security": security,
		"tag":      tag,
	}
	if transport != "" {
		reqParams["transport"] = transport
	}
	// If no domain is provided and security requires TLS, use self-signed cert
	if fastAddInboundReq.GetDomain() == "" && (security == "tls" || security == "xtls") {
		reqParams["self_signed"] = true
	}
	if fastAddInboundReq.GetRealityTarget() != "" {
		reqParams["reality_target"] = fastAddInboundReq.GetRealityTarget()
	}
	if len(fastAddInboundReq.GetRealityServerNames()) > 0 {
		reqParams["reality_server_names"] = fastAddInboundReq.GetRealityServerNames()
	}
	if len(fastAddInboundReq.GetRealityShortIds()) > 0 {
		reqParams["reality_short_ids"] = fastAddInboundReq.GetRealityShortIds()
	}

	// Merge extra_params: pass through all transport/security/sniffing params to Executor.
	// String values are passed as-is; special keys are converted to appropriate types.
	for k, v := range fastAddInboundReq.GetExtraParams() {
		switch k {
		case "sniffing_enabled", "sniffing_route_only", "tls_reject_unknown_sni", "self_signed", "skip_cert_verify":
			reqParams[k] = v == "true"
		case "ocsp_stapling":
			var n int
			fmt.Sscanf(v, "%d", &n)
			reqParams[k] = n
		case "reality_server_names", "sniffing_dest_override", "alpn", "xhttp_host", "http_host":
			reqParams[k] = strings.Split(v, ",")
		case "reality_short_ids":
			if _, exists := reqParams["reality_short_ids"]; !exists {
				reqParams[k] = strings.Split(v, ",")
			}
		default:
			// String params: ws_path, grpc_service_name, httpupgrade_path, httpupgrade_host,
			// xhttp_path, xhttp_mode, tls_min_version, flow, uuid, method, password, etc.
			reqParams[k] = v
		}
	}

	// Enforce validated canonical chain & isolation metadata after extra_params merge.
	reqParams["tag"] = tag
	reqParams["inbound_type"] = string(inboundType)
	reqParams["chain_uri"] = chainURI
	reqParams["access_tags"] = accessTags
	reqParams["remark"] = remark
	if inboundType == contracts.InboundTypeChain {
		reqParams["listen"] = "127.0.0.1"
	}

	// Container-agnostic normalisation: credential auto-fill + cert-source
	// resolution. Runs AFTER extra_params merge so caller-supplied values
	// always win. scratchDir comes from the container when it has a
	// specific requirement (mihomo: DataDir for SAFE_PATHS compliance),
	// falling back to os.TempDir for containers without one (xray's own
	// resolveFastAddCert already handles /tmp → xray-internal temp files).
	scratchDir := os.TempDir()
	if p, ok := c.(interface{ CertScratchDir() string }); ok {
		scratchDir = p.CertScratchDir()
	}
	if err := params.FillDefaults(reqParams, protocol, newCertMgrAdapter(s.certManager), scratchDir); err != nil {
		fastAddInboundRsp.Code = 1020
		fastAddInboundRsp.Msg = err.Error()
		return fastAddInboundRsp, nil
	}

	if err := c.FastAddInbound(tag, reqParams); err != nil {
		fastAddInboundRsp.Code = 1021
		fastAddInboundRsp.Msg = err.Error()
	}
	return fastAddInboundRsp, nil
}

// certMgrAdapter turns the RPC layer's CertManager interface into the
// narrow params.CertManager shape. The adapter short-circuits to nil when
// the underlying manager doesn't expose GetCertFiles (test doubles); this
// disables the domain-cert-source path gracefully rather than crashing.
type certMgrAdapter struct {
	mgr CertManager
}

func newCertMgrAdapter(mgr CertManager) params.CertManager {
	if mgr == nil {
		return nil
	}
	return &certMgrAdapter{mgr: mgr}
}

func (a *certMgrAdapter) GetCert(domain string) *params.CertRecord {
	// The concrete certmgmt.Manager exposes GetCertFiles; test doubles
	// may not. Treat missing support as "no cert available" — callers
	// that needed domain resolution will see a clean not-found error
	// from params.FillDefaults rather than a panic here.
	g, ok := a.mgr.(interface {
		GetCertFiles(domain string) (certFile, keyFile string, ok bool)
	})
	if !ok {
		return nil
	}
	cf, kf, found := g.GetCertFiles(domain)
	if !found || cf == "" || kf == "" {
		return nil
	}
	return &params.CertRecord{CertFile: cf, KeyFile: kf}
}

// ListInbound returns all inbounds across all registered containers.
// Iterates every container type registered in ContainerMgr (polymorphic) so
// new container types (e.g. mihomo) are picked up without changing this handler.
// Each InboundInfo contains the container type and the inbound's unique name (tag).
//
// Result is sorted by (container, name) so scripts diffing the API output
// between calls don't see reshuffles caused by Go map iteration order.
func (s *EndNodeServer) ListInbound(ctx context.Context, req *proto.ListInboundReq) (*proto.ListInboundRsp, error) {
	rsp := &proto.ListInboundRsp{Code: 0}

	for _, ct := range s.containerMgr.Types() {
		c, ok := s.containerMgr.Get(ct)
		if !ok {
			continue
		}
		metaProvider, hasMeta := c.(container.InboundMetaProvider)
		for _, inb := range c.ListInboundConfigs() {
			info := &proto.InboundInfo{
				Container: string(ct),
				Name:      inb.Tag(),
			}
			if hasMeta {
				if meta, ok := metaProvider.GetInboundMeta(inb.Tag()); ok {
					inboundType := meta.InboundType
					if inboundType == "" {
						inboundType = contracts.InboundTypeDirect
					}
					info.InboundType = string(inboundType)
					if len(meta.AccessTags) > 0 {
						info.AccessTags = append([]string(nil), meta.AccessTags...)
					}
					info.Remark = meta.Remark
					if inboundType == contracts.InboundTypeChain && meta.ChainOutboundURI != "" {
						info.ChainProtocol, info.ChainTarget = chain.SummarizeURI(meta.ChainOutboundURI)
					}
				} else {
					info.InboundType = string(contracts.InboundTypeDirect)
				}
			} else {
				info.InboundType = string(contracts.InboundTypeDirect)
			}
			rsp.Inbounds = append(rsp.Inbounds, info)
		}
	}

	sort.Slice(rsp.Inbounds, func(i, j int) bool {
		if rsp.Inbounds[i].GetContainer() != rsp.Inbounds[j].GetContainer() {
			return rsp.Inbounds[i].GetContainer() < rsp.Inbounds[j].GetContainer()
		}
		return rsp.Inbounds[i].GetName() < rsp.Inbounds[j].GetName()
	})

	return rsp, nil
}

// DeleteInboundByName deletes an inbound identified by container type and name (tag).
// Dispatches polymorphically via ContainerMgr — works for any registered container
// type (xray / snell / hysteria / mihomo / future).
//
// Container contract asymmetry with FastAddInbound (intentional): FastAddInbound
// treats empty ContainerType as xray for backward compatibility with older
// xray-only callers. DeleteInboundByName requires a non-empty Container — an
// accidental empty field on a destructive op must not default to the biggest
// container. Callers who genuinely want the xray container should pass "xray"
// explicitly.
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

// SetInboundAccessTags updates the Layer 2 access tags on an existing inbound.
func (s *EndNodeServer) SetInboundAccessTags(ctx context.Context, req *proto.SetInboundAccessTagsReq) (*proto.InboundOpRsp, error) {
	rsp := &proto.InboundOpRsp{Code: 0}

	containerType := strings.TrimSpace(req.GetContainer())
	if containerType == "" {
		rsp.Code = 600
		rsp.Msg = "container is required"
		return rsp, nil
	}
	name := strings.TrimSpace(req.GetName())
	if name == "" {
		rsp.Code = 600
		rsp.Msg = "name is required"
		return rsp, nil
	}
	tags, err := contracts.NormalizeAccessTags(req.GetAccessTags())
	if err != nil {
		rsp.Code = 600
		rsp.Msg = fmt.Sprintf("invalid access_tags: %v", err)
		return rsp, nil
	}

	c, err := s.getContainerByType(containerType)
	if err != nil {
		rsp.Code = 600
		rsp.Msg = err.Error()
		return rsp, nil
	}
	updater, ok := c.(container.InboundTagUpdater)
	if !ok {
		rsp.Code = 600
		rsp.Msg = fmt.Sprintf("container %s does not support access tag updates", containerType)
		return rsp, nil
	}
	if err := updater.SetInboundAccessTags(name, tags); err != nil {
		rsp.Code = 601
		rsp.Msg = fmt.Sprintf("set inbound access tags failed: %v", err)
		return rsp, nil
	}

	log.Info("SetInboundAccessTags success", "container", containerType, "name", name, "access_tags", tags)
	return rsp, nil
}

func (s *EndNodeServer) inboundTagExistsAnywhere(tag string) (contracts.ContainerType, bool) {
	if s.containerMgr == nil || tag == "" {
		return "", false
	}
	for _, ct := range s.containerMgr.Types() {
		c, ok := s.containerMgr.Get(ct)
		if !ok {
			continue
		}
		if inb, err := c.GetInboundConfig(tag); err == nil && inb != nil {
			return ct, true
		}
	}
	return "", false
}

func generateInboundTag(protocol string, inboundType contracts.InboundType) (string, error) {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	suffix := hex.EncodeToString(raw[:])
	if inboundType == contracts.InboundTypeChain {
		return fmt.Sprintf("%s-chain-%s", protocol, suffix), nil
	}
	return fmt.Sprintf("%s-%s", protocol, suffix), nil
}

func (s *EndNodeServer) buildChainAddressPolicy() chain.AddressPolicy {
	reservedPorts := map[uint32]struct{}{
		62789: {}, // xray default gRPC API
		9999:  {}, // hysteria default traffic stats API
		9090:  {}, // mihomo default external controller
	}
	if s.cfg.HttpPort > 0 {
		reservedPorts[uint32(s.cfg.HttpPort)] = struct{}{}
	}
	if s.cfg.RpcPort > 0 {
		reservedPorts[uint32(s.cfg.RpcPort)] = struct{}{}
	}
	if s.Port > 0 {
		reservedPorts[uint32(s.Port)] = struct{}{}
	}

	policy := chain.AddressPolicy{
		AllowLoopback: s.cfg.AllowLoopbackChainTarget,
		LocalHosts:    collectLocalHosts(s.cfg, s.Host),
		ReservedPorts: reservedPorts,
	}
	if s.userMgr != nil {
		if fm := s.userMgr.GetForwardManager(); fm != nil {
			policy.IsPortAllocated = func(port uint32) bool {
				if checker, ok := fm.(interface{ IsPortAllocated(uint32) bool }); ok {
					return checker.IsPortAllocated(port)
				}
				if checker, ok := fm.(interface{ IsAllocated(uint32) bool }); ok {
					return checker.IsAllocated(port)
				}
				for _, r := range fm.GetAllRules() {
					if r.ListenPort == port {
						return true
					}
				}
				return false
			}
		}
	}
	return policy
}

func collectLocalHosts(cfg appconfig.EndNodeConfig, serverHost string) []string {
	seen := make(map[string]struct{})
	var hosts []string
	addHost := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if host, _, err := net.SplitHostPort(raw); err == nil {
			raw = strings.TrimSpace(host)
		}
		raw = strings.Trim(raw, "[]")
		if raw == "" || raw == "0.0.0.0" || raw == "::" {
			return
		}
		key := strings.ToLower(raw)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		hosts = append(hosts, raw)
	}

	addHost(cfg.ProxyHost)
	addHost(cfg.Listen)
	addHost(cfg.HttpListen)
	addHost(serverHost)

	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsUnspecified() {
				continue
			}
			addHost(ip.String())
		}
	}
	return hosts
}
