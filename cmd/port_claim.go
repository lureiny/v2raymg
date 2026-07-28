package cmd

import (
	"net"
	"strconv"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/store"
)

// Defaults for management ports the containers apply in their own Decode().
// Duplicated here because reservation has to happen before LoadFromConfig
// decodes those configs, and the decoded types are container-specific. Keep in
// sync with:
//   - xray:      pkg/proxy/containers/xray/register.go     (GRPCAPIAddress)
//   - hysteria:  pkg/proxy/containers/hysteria/config.go   (TrafficStatsPort)
//   - mihomo:    pkg/proxy/containers/mihomo/config.go     (ExternalController)
//
// Reserving a port the container does not actually use is harmless; failing to
// reserve one it does use is not — see the xray note in reservedManagementPorts.
const (
	defaultXrayGRPCPort           = 62789
	defaultHysteriaStatsPort      = 9999
	defaultMihomoExternalCtrlPort = 9090
)

// reservedManagementPorts lists the ports this process and its child proxies
// bind for control purposes. They are reserved rather than claimed because
// nothing ever releases them: they live as long as the process does.
//
// The xray gRPC API port is the one that must not be missed. xray's Start calls
// killProcessOnPort on it (pkg/proxy/containers/xray/exec_runner.go), which
// SIGKILLs whatever holds that port — it checks only for pid <= 1, not for the
// process name, and not for our own pid. If a forward relay were ever handed
// 62789, starting the xray container would kill v2raymg itself.
func reservedManagementPorts(cfg *appconfig.AppConfig) []uint32 {
	var ports []uint32
	add := func(p int) {
		if p > 0 && p <= 65535 {
			ports = append(ports, uint32(p))
		}
	}

	add(cfg.EndNode.RpcPort)
	add(cfg.EndNode.HttpPort)

	for _, entry := range cfg.Containers.Containers {
		switch entry.Type {
		case contracts.ContainerXray:
			add(portFromAddrValue(entry.Config["grpc_port"], defaultXrayGRPCPort))
		case contracts.ContainerHysteria:
			add(intFromConfigValue(entry.Config["traffic_stats_port"], defaultHysteriaStatsPort))
		case contracts.ContainerMihomo:
			add(portFromHostPortValue(entry.Config["external_controller"], defaultMihomoExternalCtrlPort))
		}
	}
	return ports
}

// portClaimReport summarises what the startup pre-claim pass found. Returned
// rather than only logged so a later "inbound health" surface can report the
// conflicts without re-deriving them.
type portClaimReport struct {
	claimed    int
	conflicts  []portConflict
	unreadable []string
}

// portConflict is two inbound records wanting the same port. Almost always a
// leftover from when an omitted port silently became a hardcoded 10000, so the
// second record has never been able to bind anyway.
type portConflict struct {
	tag       string
	port      uint32
	holderTag string
}

// preClaimPersistedInboundPorts records every port a persisted inbound will
// want back, plus the fixed data ports of the single-inbound containers, so
// that no forward rule allocated later in startup can take one.
//
// Conflicts do not stop the boot. An existing database may well already contain
// two inbounds on the same port, and refusing to start would strand a node on
// data it cannot fix without the node running. The losing record is left
// untouched — its configuration is still there to repair — and reported loudly.
func preClaimPersistedInboundPorts(
	storeMgr *store.StoreManager,
	claimer container.PortClaimer,
	cfg *appconfig.AppConfig,
) portClaimReport {
	var report portClaimReport

	// Keyed by port. The container type is carried alongside the tag to
	// recognise one specific non-conflict: hysteria and snell reach this pass
	// twice — once via their persisted record (tagged "<name>-default") and
	// once via their configured port — and those are the same listener.
	//
	// That exemption is deliberately narrow. It applies ONLY to the configured
	// port of a single-inbound container matching its own record. Two records
	// of the same container type on one port is a real conflict and the one we
	// most need to report: it is what a pair of mihomo inbounds created without
	// an explicit port used to look like, both rewritten to the same constant.
	type holder struct {
		tag           string
		containerType string
	}
	holders := map[uint32]holder{}

	// Ports already in the reserved set are protected by construction — the
	// allocator will never draw them — so a record sitting on one is fine, not
	// a conflict. xray's "api" inbound is the real case: it is rendered onto
	// the gRPC API port (reserved above) and can end up persisted, and without
	// this it would log a conflict on every single boot.
	reserved := map[uint32]bool{}
	for _, p := range reservedManagementPorts(cfg) {
		reserved[p] = true
	}

	claim := func(port uint32, tag, containerType string, singleInboundConfig bool) {
		if port == 0 || reserved[port] {
			return
		}
		if h, taken := holders[port]; taken {
			if singleInboundConfig && containerType != "" && h.containerType == containerType {
				return // this container's own record, seen twice
			}
			report.conflicts = append(report.conflicts, portConflict{
				tag: tag, port: port, holderTag: h.tag,
			})
			log.Error("two inbounds are configured on the same port; only the first can bind",
				"port", port, "holder", h.tag, "conflicting", tag,
				"hint", "give one of them a different port and restart")
			return
		}
		if err := claimer.AllocateSpecificPort(port); err != nil {
			report.conflicts = append(report.conflicts, portConflict{tag: tag, port: port})
			log.Error("could not reserve a persisted inbound port; it may be handed to a forward rule",
				"tag", tag, "port", port, "err", err)
			return
		}
		holders[port] = holder{tag: tag, containerType: containerType}
		report.claimed++
	}

	if storeMgr != nil {
		records, err := storeMgr.InboundStore().Load()
		if err != nil {
			// Without the record list we cannot protect any persisted port.
			// Boot anyway — the containers still restore from the same store
			// and will surface their own errors — but be explicit that the
			// protection is off for this run.
			log.Error("could not load persisted inbounds; inbound ports are unprotected this run "+
				"and a forward rule may take one", "err", err)
			return report
		}
		for _, rec := range records {
			port, ok := container.ParsePersistedPort(rec)
			if !ok {
				report.unreadable = append(report.unreadable, rec.Tag)
				log.Warn("could not read the listen port out of a persisted inbound; "+
					"it will not be protected against reuse",
					"tag", rec.Tag, "container", rec.ContainerType)
				continue
			}
			claim(port, rec.Tag, rec.ContainerType, false)
		}
	}

	// hysteria and snell take their single inbound's port from container config
	// rather than from the API, and on a fresh install there is no record yet.
	// Claim the configured value too; when it matches that container's own
	// record it is deduped by container type above.
	for _, entry := range cfg.Containers.Containers {
		switch entry.Type {
		case contracts.ContainerHysteria, contracts.ContainerSnell:
			if p := intFromConfigValue(entry.Config["port"], 0); p > 0 {
				claim(uint32(p), string(entry.Type)+" (configured port)", string(entry.Type), true)
			}
		}
	}

	if n := len(report.conflicts); n > 0 {
		log.Error("startup port pre-claim finished with conflicts", "conflicts", n, "claimed", report.claimed)
	} else {
		log.Info("startup port pre-claim finished", "claimed", report.claimed)
	}
	return report
}

// intFromConfigValue reads a numeric value out of an undecoded container config
// map. YAML gives int, JSON gives float64; accept both, like the container
// Decode implementations do.
func intFromConfigValue(v any, fallback int) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return fallback
}

// portFromAddrValue reads xray's grpc_port, which is a bare port number.
func portFromAddrValue(v any, fallback int) int {
	return intFromConfigValue(v, fallback)
}

// portFromHostPortValue reads a "host:port" string such as mihomo's
// external_controller.
func portFromHostPortValue(v any, fallback int) int {
	s, ok := v.(string)
	if !ok || s == "" {
		return fallback
	}
	_, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return fallback
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 || p > 65535 {
		return fallback
	}
	return p
}

// compile-time assertion that the production forward manager satisfies the
// narrow authority interface containers depend on.
var _ container.PortClaimer = (*forward.DefaultForwardManager)(nil)
