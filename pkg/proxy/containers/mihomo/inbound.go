package mihomo

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// MihomoInbound represents a single mihomo listener managed by v2raymg.
//
// Per docs/mihomo-container-design.md the model is "one listener per inbound,
// one shared credential per listener": all users authenticate with the same
// UUID / password / password+cipher; per-user isolation is done at the
// forward layer, not at the listener. See docs/container-design-principles.md
// principle 3 for why this matches xray/snell rather than mihomo's native
// per-listener users array.
type MihomoInbound struct {
	*inbound.DefaultInbound

	// SharedCred carries the single credential written into the mihomo
	// listener yaml. Only the fields matching the protocol are consulted:
	//   vmess:        UUID
	//   trojan:       Password
	//   shadowsocks:  Password, Cipher
	//
	// MVP three protocols (vmess/trojan/ss) continue to use this field.
	// Phase 1+ new protocols (vless/hy2/tuic/anytls) use ProtocolParams
	// below; SharedCred stays empty for them. As each MVP protocol gets
	// its advanced-feature upgrade (Phase 2/3/4), the struct migrates to
	// the ProtocolParams form; SharedCred can be retired once all branches
	// are moved.
	SharedCred MihomoSharedCred `json:"shared_cred"`

	// ProtocolParams carries the structured protocol/transport/security
	// configuration for protocols wired through the new protocolparams
	// layer. nil for MVP three protocols that still go through
	// FromParams → SharedCred. profilegen / subscription inspect Protocol()
	// and pull from either SharedCred or ProtocolParams accordingly.
	ProtocolParams *protocolparams.ProtocolParams `json:"protocol_params,omitempty"`

	// userMgr is injected by the container for AddUser / RemoveUser /
	// ReleaseAllUserPorts. nil is tolerated for unit tests that only
	// exercise the credential/validation layer.
	userMgr *usermanager.UserManager

	// addedUsers records which users have been wired up on this inbound
	// in the current process lifecycle. Used as a fast-path skip in
	// reconcile; the forward layer is the source of truth. See xray's
	// XrayInbound.addedUsers for the same contract — all access goes
	// through the helper methods below.
	addedUsersMu sync.Mutex
	addedUsers   map[string]struct{}
}

// MihomoSharedCred holds the union of per-protocol credential fields. A
// typed struct keeps JSON serialisation stable and lets Validate enumerate
// required fields per protocol without a map[string]any detour.
//
// CertFile / KeyFile are trojan-specific: mihomo Alpha rejects a trojan
// listener at boot time without "certificates/reality/ss" configured
// (observed in stage 11+ E2E: "disallow using Trojan without both
// certificates/reality/ss config"). Both must be non-empty together or
// both empty; Validate enforces this. They map to the mihomo yaml
// "certificate" / "private-key" inbound fields, each expecting an absolute
// path to a PEM file on disk.
type MihomoSharedCred struct {
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	Cipher   string `json:"cipher,omitempty"`
	CertFile string `json:"cert_file,omitempty"`
	KeyFile  string `json:"key_file,omitempty"`
	// CertSource records where CertFile/KeyFile came from. It governs
	// whether RemoveInboundConfig deletes the files on the way out — we
	// only clean up material v2raymg itself wrote to the scratch dir.
	// Possible values:
	//   ""            — unset / no cert (non-trojan protocols)
	//   "file"        — caller supplied absolute paths; do NOT delete
	//   "domain"      — paths came from certmgr; do NOT delete (certmgr owns)
	//   "pem"         — v2raymg wrote the files from user-supplied PEM content
	//                   → DELETE on inbound removal
	//   "self_signed" — v2raymg generated the files on demand
	//                   → DELETE on inbound removal
	// Written by the RPC normalisation layer (pkg/proxy/core/params) and
	// echoed through the Adapter; empty strings on legacy records are
	// treated as "file" (safe-by-default: don't delete unknown files).
	CertSource string `json:"cert_source,omitempty"`
}

func shouldCleanupCertSource(src string) bool {
	return src == "pem" || src == "self_signed"
}

// shouldCleanupCerts reports whether RemoveInboundConfig may remove the
// cert/key files on disk. True only for sources v2raymg itself writes
// ("pem" and "self_signed"); externally-managed sources are left alone
// to avoid accidentally nuking caller-maintained material or certmgr's
// renewal state.
func (c MihomoSharedCred) shouldCleanupCerts() bool {
	return shouldCleanupCertSource(c.CertSource)
}

// cleanupCertFiles returns cert files owned by v2raymg. ProtocolParams and
// SharedCred are mutually exclusive for normal records; the priority order is
// defensive for hand-built or migration-intermediate values.
func (i *MihomoInbound) cleanupCertFiles() []string {
	if i.ProtocolParams != nil &&
		i.ProtocolParams.Security != nil &&
		i.ProtocolParams.Security.Kind == contracts.SecurityTLS &&
		i.ProtocolParams.Security.TLS != nil &&
		shouldCleanupCertSource(i.ProtocolParams.Security.TLS.CertSource) {
		return []string{
			i.ProtocolParams.Security.TLS.CertFile,
			i.ProtocolParams.Security.TLS.KeyFile,
		}
	}
	if i.SharedCred.shouldCleanupCerts() {
		return []string{i.SharedCred.CertFile, i.SharedCred.KeyFile}
	}
	return nil
}

// Errors surfaced by the mihomo inbound layer. Callers can use errors.Is to
// branch on these when they need to (e.g. HTTP handlers returning distinct
// status codes for "unknown protocol" vs "missing field").
var (
	// ErrProtocolNotSupported is returned when an inbound targets a protocol
	// outside the contracts.Protocol set the mihomo container has wired.
	// As of Phase 7 (AnyTLS) every contracts.Protocol has a container
	// branch, so this surfaces only for unknown / future protocol strings.
	// See D4 in docs/mihomo-container-implementation-plan.md.
	ErrProtocolNotSupported = errors.New("mihomo: protocol not supported (supported: vless/vmess/trojan/shadowsocks/hysteria2/tuic/anytls)")

	// ErrMissingCredential is returned when a required protocol-specific
	// credential field is empty.
	ErrMissingCredential = errors.New("mihomo: required credential missing")
)

// NewMihomoInbound creates a MihomoInbound with the loopback listen address
// for the MVP three protocols (vmess/trojan/shadowsocks) whose credential
// material fits into MihomoSharedCred.
//
// listen is hardcoded to 127.0.0.1 because external ingress is handled by
// the forward layer; the mihomo process itself must not be reachable from
// the public interface.
func NewMihomoInbound(tag string, protocol contracts.Protocol, port uint32, cred MihomoSharedCred) *MihomoInbound {
	base := inbound.NewDefaultInbound(tag, protocol, port)
	base.SetListenAddr("127.0.0.1")
	return &MihomoInbound{
		DefaultInbound: base,
		SharedCred:     cred,
		addedUsers:     make(map[string]struct{}),
	}
}

// NewMihomoInboundFromProtocolParams is the Phase 1+ entry point — creates
// a MihomoInbound whose protocol-specific shape is carried in
// *ProtocolParams instead of SharedCred. Used by vless / hy2 / tuic /
// anytls and, eventually, the migrated forms of vmess / trojan / ss.
func NewMihomoInboundFromProtocolParams(pp *protocolparams.ProtocolParams) *MihomoInbound {
	base := inbound.NewDefaultInbound(pp.Tag, pp.Protocol, pp.Port)
	listen := pp.ListenAddr
	if listen == "" {
		listen = "127.0.0.1"
	}
	base.SetListenAddr(listen)
	return &MihomoInbound{
		DefaultInbound: base,
		ProtocolParams: pp,
		addedUsers:     make(map[string]struct{}),
	}
}

// Validate performs structural validation plus per-protocol credential checks.
// Returns ErrProtocolNotSupported or ErrMissingCredential (wrapped with
// field-level context) on failure.
//
// MVP three protocols (vmess/trojan/ss) validate their SharedCred. Phase 1+
// protocols (vless first) validate ProtocolParams; when both happen to be
// populated, the ProtocolParams route takes precedence (future migrations
// will drop SharedCred but the coexistence is intentional until then).
func (i *MihomoInbound) Validate() error {
	if err := i.DefaultInbound.Validate(); err != nil {
		return err
	}
	switch i.Protocol() {
	case contracts.ProtocolVLess:
		return i.validateVLESS()
	case contracts.ProtocolVMess:
		return i.validateVMess()
	case contracts.ProtocolTrojan:
		return i.validateTrojan()
	case contracts.ProtocolShadowsocks:
		return i.validateSS()
	case contracts.ProtocolHysteria2:
		return i.validateHysteria2()
	case contracts.ProtocolTUIC:
		return i.validateTuic()
	case contracts.ProtocolAnyTLS:
		return i.validateAnyTLS()
	default:
		return fmt.Errorf("%w: %q", ErrProtocolNotSupported, i.Protocol())
	}
}

// validateAnyTLS enforces the Phase 7 invariants. AnyTLS has no legacy
// SharedCred path — Phase 7 is its first appearance in this container,
// so ProtocolParams.AnyTLS is mandatory.
//
// Rules overlap with parseAnyTLS (TLS-only, password required, idle/min
// non-negative). The duplication is intentional: Validate is the single
// gate FromNative records cross when reloaded from InboundStore, and a
// stored record may carry combinations that never went through Parse.
//
// Sentinel choice notes (mirrors validateHysteria2 / validateTuic): this
// package only exposes ErrMissingCredential and ErrProtocolNotSupported.
// "Negative idle" is logically an invalid value (parser uses
// ErrInvalidCombination) but at this layer it folds into
// ErrProtocolNotSupported — the asymmetry is inherited project-wide; if
// you ever introduce ErrInvalidConfig it should be applied to all three
// validateXxx functions in one pass, not to AnyTLS in isolation.
func (i *MihomoInbound) validateAnyTLS() error {
	if i.ProtocolParams == nil || i.ProtocolParams.AnyTLS == nil {
		return fmt.Errorf("%w: anytls inbound missing ProtocolParams.AnyTLS", ErrMissingCredential)
	}
	a := i.ProtocolParams.AnyTLS
	if a.Password == "" {
		return fmt.Errorf("%w: anytls requires password", ErrMissingCredential)
	}
	sec := i.ProtocolParams.Security
	if sec == nil {
		return fmt.Errorf("%w: anytls requires tls security", ErrMissingCredential)
	}
	if sec.Kind != contracts.SecurityTLS {
		return fmt.Errorf("%w: anytls security must be tls, got %q", ErrProtocolNotSupported, sec.Kind)
	}
	if sec.TLS == nil {
		return fmt.Errorf("%w: anytls security=tls but TLS spec is nil", ErrMissingCredential)
	}
	if sec.TLS.CertFile == "" || sec.TLS.KeyFile == "" {
		return fmt.Errorf("%w: anytls requires cert_file and key_file", ErrMissingCredential)
	}
	if a.IdleSessionCheckInterval < 0 {
		return fmt.Errorf("%w: anytls idle_session_check_interval_seconds must be >= 0", ErrProtocolNotSupported)
	}
	if a.IdleSessionTimeout < 0 {
		return fmt.Errorf("%w: anytls idle_session_timeout_seconds must be >= 0", ErrProtocolNotSupported)
	}
	if a.MinIdleSession < 0 {
		return fmt.Errorf("%w: anytls min_idle_session must be >= 0", ErrProtocolNotSupported)
	}
	return nil
}

// validateTuic enforces the Phase 6 invariants. TUIC has no legacy SharedCred
// path — Phase 6 is its first appearance in the mihomo container, so
// ProtocolParams.TUIC is mandatory.
//
// Several rules here overlap with parseTUIC (TLS-only, congestion-controller
// whitelist, udp-relay-mode whitelist, valid uuid). The duplication is
// deliberate — Validate is the single gate FromNative records cross when
// reloaded from InboundStore, and a stored record may carry combinations
// that never went through Parse (hand-edited or migration-intermediate).
func (i *MihomoInbound) validateTuic() error {
	if i.ProtocolParams == nil || i.ProtocolParams.TUIC == nil {
		return fmt.Errorf("%w: tuic inbound missing ProtocolParams.TUIC", ErrMissingCredential)
	}
	t := i.ProtocolParams.TUIC
	if t.UUID == "" {
		return fmt.Errorf("%w: tuic requires uuid", ErrMissingCredential)
	}
	if t.Password == "" {
		return fmt.Errorf("%w: tuic requires password", ErrMissingCredential)
	}
	sec := i.ProtocolParams.Security
	if sec == nil {
		return fmt.Errorf("%w: tuic requires tls security", ErrMissingCredential)
	}
	if sec.Kind != contracts.SecurityTLS {
		return fmt.Errorf("%w: tuic security must be tls, got %q", ErrProtocolNotSupported, sec.Kind)
	}
	if sec.TLS == nil {
		return fmt.Errorf("%w: tuic security=tls but TLS spec is nil", ErrMissingCredential)
	}
	if sec.TLS.CertFile == "" || sec.TLS.KeyFile == "" {
		return fmt.Errorf("%w: tuic requires cert_file and key_file", ErrMissingCredential)
	}
	switch t.CongestionController {
	case "", "bbr", "cubic", "new_reno":
		// "" is acceptable here — fillTuicListener writes "bbr" as the default.
	default:
		return fmt.Errorf("%w: tuic congestion_controller %q not recognised",
			ErrProtocolNotSupported, t.CongestionController)
	}
	switch t.UDPRelayMode {
	case "", "native", "quic":
		// ok
	default:
		return fmt.Errorf("%w: tuic udp_relay_mode %q not recognised",
			ErrProtocolNotSupported, t.UDPRelayMode)
	}
	return nil
}

// validateHysteria2 enforces the Phase 5 invariants. Hysteria2 has no legacy
// SharedCred path — Phase 5 is its first appearance in the mihomo container,
// so ProtocolParams.Hysteria2 is mandatory.
//
// Several rules here overlap with parseHysteria2 (TLS-only, obfs ∈ {"",
// "salamander"}, salamander requires obfs_password). The duplication is
// deliberate: Validate is the single gate FromNative records cross when
// reloaded from InboundStore, and a record on disk could carry combinations
// that never went through Parse — either from a hand-edited store row or
// from a future migration that bypasses the parser. Keep this function
// self-sufficient rather than trusting Parse already ran.
func (i *MihomoInbound) validateHysteria2() error {
	if i.ProtocolParams == nil || i.ProtocolParams.Hysteria2 == nil {
		return fmt.Errorf("%w: hysteria2 inbound missing ProtocolParams.Hysteria2", ErrMissingCredential)
	}
	hy2 := i.ProtocolParams.Hysteria2
	if hy2.Password == "" {
		return fmt.Errorf("%w: hysteria2 requires password", ErrMissingCredential)
	}
	sec := i.ProtocolParams.Security
	if sec == nil {
		return fmt.Errorf("%w: hysteria2 requires tls security", ErrMissingCredential)
	}
	if sec.Kind != contracts.SecurityTLS {
		return fmt.Errorf("%w: hysteria2 security must be tls, got %q", ErrProtocolNotSupported, sec.Kind)
	}
	if sec.TLS == nil {
		return fmt.Errorf("%w: hysteria2 security=tls but TLS spec is nil", ErrMissingCredential)
	}
	if sec.TLS.CertFile == "" || sec.TLS.KeyFile == "" {
		return fmt.Errorf("%w: hysteria2 requires cert_file and key_file", ErrMissingCredential)
	}
	// Unrecognised obfs uses ErrProtocolNotSupported because this package
	// only exposes two sentinels (ErrProtocolNotSupported / ErrMissingCredential)
	// and "this obfs implementation isn't wired up" reads closer to the
	// "not supported" frame than to "missing credential". The parser layer
	// has a richer ErrInvalidCombination but Validate is mihomo-package-local.
	if hy2.Obfs != "" && hy2.Obfs != "salamander" {
		return fmt.Errorf("%w: hysteria2 obfs %q not recognised", ErrProtocolNotSupported, hy2.Obfs)
	}
	if hy2.Obfs == "salamander" && hy2.ObfsPassword == "" {
		return fmt.Errorf("%w: hysteria2 obfs=salamander requires obfs_password", ErrMissingCredential)
	}
	return nil
}

// validateSS dispatches between the Phase 4 ProtocolParams path and the legacy
// SharedCred path. ProtocolParams takes precedence when present.
func (i *MihomoInbound) validateSS() error {
	if i.ProtocolParams != nil && i.ProtocolParams.SS != nil {
		ss := i.ProtocolParams.SS
		if ss.Password == "" {
			return fmt.Errorf("%w: shadowsocks requires password", ErrMissingCredential)
		}
		if ss.Cipher == "" {
			return fmt.Errorf("%w: shadowsocks requires cipher", ErrMissingCredential)
		}
		return nil
	}
	// Legacy SharedCred path.
	if i.SharedCred.Password == "" {
		return fmt.Errorf("%w: shadowsocks requires password", ErrMissingCredential)
	}
	if i.SharedCred.Cipher == "" {
		return fmt.Errorf("%w: shadowsocks requires cipher", ErrMissingCredential)
	}
	return nil
}

// validateTrojan dispatches between the Phase 3 ProtocolParams path and the
// legacy SharedCred path. Legacy records may omit certs (mihomo runtime will
// reject such listeners); the structured path keeps the same pair-check while
// allowing TLS material to come from SecuritySpec.
func (i *MihomoInbound) validateTrojan() error {
	if i.ProtocolParams != nil && i.ProtocolParams.Trojan != nil {
		if i.ProtocolParams.Trojan.Password == "" {
			return fmt.Errorf("%w: trojan requires password", ErrMissingCredential)
		}
		if i.ProtocolParams.Security == nil {
			return fmt.Errorf("%w: trojan requires tls or reality security", ErrMissingCredential)
		}
		switch sec := i.ProtocolParams.Security; sec.Kind {
		case contracts.SecurityTLS:
			if sec.TLS == nil {
				return fmt.Errorf("%w: trojan security=tls but TLS spec is nil", ErrMissingCredential)
			}
			if (sec.TLS.CertFile == "") != (sec.TLS.KeyFile == "") {
				return fmt.Errorf("%w: trojan cert_file and key_file must be set together", ErrMissingCredential)
			}
		case contracts.SecurityReality:
			if sec.Reality == nil {
				return fmt.Errorf("%w: trojan security=reality but Reality spec is nil", ErrMissingCredential)
			}
			if sec.Reality.Target == "" {
				return fmt.Errorf("%w: trojan reality target required", ErrMissingCredential)
			}
			if len(sec.Reality.ServerNames) == 0 {
				return fmt.Errorf("%w: trojan reality server_names required", ErrMissingCredential)
			}
		default:
			return fmt.Errorf("%w: trojan security %q not supported", ErrProtocolNotSupported, sec.Kind)
		}
		return nil
	}
	if i.SharedCred.Password == "" {
		return fmt.Errorf("%w: trojan requires password", ErrMissingCredential)
	}
	if (i.SharedCred.CertFile == "") != (i.SharedCred.KeyFile == "") {
		return fmt.Errorf("%w: trojan cert_file and key_file must be set together", ErrMissingCredential)
	}
	return nil
}

// validateVMess dispatches between the new ProtocolParams path (Phase 2+)
// and the legacy SharedCred path. Records written before Phase 2 have
// ProtocolParams=nil and fall through to the SharedCred.UUID check —
// same rule that shipped in Phase 0, minus the inline style. Phase 2
// records and later carry pp.VMess; those validate via the structured
// security/transport slots.
func (i *MihomoInbound) validateVMess() error {
	if i.ProtocolParams != nil && i.ProtocolParams.VMess != nil {
		if i.ProtocolParams.VMess.UUID == "" {
			return fmt.Errorf("%w: vmess requires uuid", ErrMissingCredential)
		}
		if sec := i.ProtocolParams.Security; sec != nil {
			switch sec.Kind {
			case contracts.SecurityTLS:
				if sec.TLS == nil {
					return fmt.Errorf("%w: vmess security=tls but TLS spec is nil", ErrMissingCredential)
				}
				if (sec.TLS.CertFile == "") != (sec.TLS.KeyFile == "") {
					return fmt.Errorf("%w: vmess cert_file and key_file must be set together", ErrMissingCredential)
				}
			case contracts.SecurityReality:
				if sec.Reality == nil {
					return fmt.Errorf("%w: vmess security=reality but Reality spec is nil", ErrMissingCredential)
				}
				if sec.Reality.Target == "" {
					return fmt.Errorf("%w: vmess reality target required", ErrMissingCredential)
				}
				if len(sec.Reality.ServerNames) == 0 {
					return fmt.Errorf("%w: vmess reality server_names required", ErrMissingCredential)
				}
			}
		}
		return nil
	}
	// Legacy SharedCred path — pre-Phase-2 records.
	if i.SharedCred.UUID == "" {
		return fmt.Errorf("%w: vmess requires uuid", ErrMissingCredential)
	}
	return nil
}

// validateVLESS checks the ProtocolParams-carried configuration that vless
// inbounds need. Split out of Validate to keep the top-level switch
// readable; the logic is exclusively concerned with VLESS-specific
// invariants.
func (i *MihomoInbound) validateVLESS() error {
	if i.ProtocolParams == nil || i.ProtocolParams.VLESS == nil {
		return fmt.Errorf("%w: vless inbound missing ProtocolParams.VLESS", ErrMissingCredential)
	}
	if i.ProtocolParams.VLESS.UUID == "" {
		return fmt.Errorf("%w: vless requires uuid", ErrMissingCredential)
	}
	if sec := i.ProtocolParams.Security; sec != nil {
		switch sec.Kind {
		case contracts.SecurityTLS:
			if sec.TLS == nil {
				return fmt.Errorf("%w: vless security=tls but TLS spec is nil", ErrMissingCredential)
			}
			if (sec.TLS.CertFile == "") != (sec.TLS.KeyFile == "") {
				return fmt.Errorf("%w: vless cert_file and key_file must be set together", ErrMissingCredential)
			}
		case contracts.SecurityReality:
			if sec.Reality == nil {
				return fmt.Errorf("%w: vless security=reality but Reality spec is nil", ErrMissingCredential)
			}
			if sec.Reality.Target == "" {
				return fmt.Errorf("%w: vless reality target required", ErrMissingCredential)
			}
			if len(sec.Reality.ServerNames) == 0 {
				return fmt.Errorf("%w: vless reality server_names required", ErrMissingCredential)
			}
		}
	}
	return nil
}

// mihomoInboundJSON is the on-disk representation written to InboundStore.
// Backward-compatible shape:
//   - SharedCred's omitempty tags keep vmess records from carrying cipher, etc.
//   - ProtocolParams is new in Phase 1; old records (MVP three protocols)
//     reload with ProtocolParams=nil and fall through Validate's
//     SharedCred branches.
//   - A record may carry both (future migration intermediate state); the
//     Validate switch picks SharedCred or ProtocolParams based on Protocol().
type mihomoInboundJSON struct {
	Tag            string                         `json:"tag"`
	Protocol       string                         `json:"protocol"`
	Port           uint32                         `json:"port"`
	ListenAddr     string                         `json:"listen_addr"`
	SharedCred     MihomoSharedCred               `json:"shared_cred"`
	ProtocolParams *protocolparams.ProtocolParams `json:"protocol_params,omitempty"`
}

// ToNative serialises the inbound for InboundStore.NativeJSON. Overrides
// DefaultInbound.ToNative (which returns ErrInboundToNativeNotImplemented).
func (i *MihomoInbound) ToNative() ([]byte, error) {
	return json.Marshal(&mihomoInboundJSON{
		Tag:            i.Tag(),
		Protocol:       string(i.Protocol()),
		Port:           i.Port(),
		ListenAddr:     i.ListenAddr(),
		SharedCred:     i.SharedCred,
		ProtocolParams: i.ProtocolParams,
	})
}

// FromNative reconstructs a MihomoInbound from a NativeJSON blob. Returns an
// error if the payload fails to decode or validate — callers (restore path)
// should log and skip rather than abort restart, so that one bad record
// doesn't sink the container.
func FromNative(data []byte) (*MihomoInbound, error) {
	var j mihomoInboundJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("mihomo: unmarshal inbound record: %w", err)
	}
	var inb *MihomoInbound
	if j.ProtocolParams != nil {
		// Phase 1+ record. Prefer ProtocolParams; honour the record's
		// Tag/Protocol/Port over pp's since those are the authoritative
		// identifiers on disk and pp is recomputed on FastAdd but persisted
		// lazily.
		pp := j.ProtocolParams
		pp.Tag = j.Tag
		pp.Protocol = contracts.Protocol(j.Protocol)
		pp.Port = j.Port
		if j.ListenAddr != "" {
			pp.ListenAddr = j.ListenAddr
		}
		inb = NewMihomoInboundFromProtocolParams(pp)
		// Deliberately do NOT copy j.SharedCred onto the new-path
		// inbound: a legacy migration artefact (Protocol=vless with
		// SharedCred lingering from a previous serialisation) would
		// slip past validateVLESS and pollute subsequent ToNative
		// calls. SharedCred only belongs to the MVP three-protocol
		// path; new-path records leave it zero.
	} else {
		// Legacy (MVP three) record.
		inb = NewMihomoInbound(j.Tag, contracts.Protocol(j.Protocol), j.Port, j.SharedCred)
		if j.ListenAddr != "" {
			inb.DefaultInbound.SetListenAddr(j.ListenAddr)
		}
	}
	if err := inb.Validate(); err != nil {
		return nil, err
	}
	return inb, nil
}

// SetUserManager wires the container's user manager into this inbound so
// AddUser/RemoveUser can allocate/release forward ports. Safe to call once
// per inbound instance; subsequent calls overwrite without coordination.
func (i *MihomoInbound) SetUserManager(um *usermanager.UserManager) {
	i.userMgr = um
}

// forwardNetworkForProtocol returns the forward-layer transport ("tcp"|"udp")
// that the given protocol's listener requires. The forward layer routes the
// per-user public port to 127.0.0.1:<inbound-port>; QUIC-based protocols
// must use UDPRelay or the relay won't pass packets through. SS UDP is a
// per-user wrap inside the SS protocol stream, NOT a separate forward port,
// so SS stays on the TCP default.
//
// Returning "" preserves the legacy default of TCP that pre-Phase-5 callers
// (vless/vmess/trojan/ss) relied on. AnyTLS (Phase 7) is TCP-based and
// stays on the default branch.
func forwardNetworkForProtocol(p contracts.Protocol) string {
	switch p {
	case contracts.ProtocolHysteria2, contracts.ProtocolTUIC:
		return "udp"
	default:
		return ""
	}
}

// hasAddedUser reports whether the given user is already tracked on this inbound.
func (i *MihomoInbound) hasAddedUser(username string) bool {
	i.addedUsersMu.Lock()
	defer i.addedUsersMu.Unlock()
	_, ok := i.addedUsers[username]
	return ok
}

// listAddedUsers returns a snapshot of tracked usernames. Callers may iterate
// the result without holding the lock.
func (i *MihomoInbound) listAddedUsers() []string {
	i.addedUsersMu.Lock()
	defer i.addedUsersMu.Unlock()
	out := make([]string, 0, len(i.addedUsers))
	for u := range i.addedUsers {
		out = append(out, u)
	}
	return out
}

func (i *MihomoInbound) markAddedUser(username string) {
	i.addedUsersMu.Lock()
	defer i.addedUsersMu.Unlock()
	i.addedUsers[username] = struct{}{}
}

func (i *MihomoInbound) unmarkAddedUser(username string) {
	i.addedUsersMu.Lock()
	defer i.addedUsersMu.Unlock()
	delete(i.addedUsers, username)
}

// AddUser wires a user to this inbound by allocating a forward port via
// UserManager.GetBindPort. It does NOT touch the mihomo listener itself —
// under the shared-credential model all users authenticate with the single
// cred in SharedCred, and per-user isolation is entirely at the forward
// layer. The returned port is the public listen port in the forward layer
// that relays to 127.0.0.1:<inbound-port>.
//
// Idempotent: if the user is already tracked AND GetUserPortByDst can still
// resolve the mapping, returns the existing port without allocating.
// Stale tracking (mapping gone after a crash) falls through to re-allocate.
func (i *MihomoInbound) AddUser(username string, _ *contracts.User) (uint32, error) {
	if i.userMgr == nil {
		return 0, fmt.Errorf("mihomo: inbound %q has no user manager", i.Tag())
	}

	if i.hasAddedUser(username) {
		if port, ok := i.userMgr.GetUserPortByDst(username, i.Port()); ok {
			return port, nil
		}
		// Forward rule vanished since we last saw it (e.g. crash); drop the
		// stale flag and fall through to re-allocate.
		i.unmarkAddedUser(username)
	}

	bindPort, err := i.userMgr.GetBindPort(usermanager.GetBindPortRequest{
		Username:      username,
		TargetPort:    i.Port(),
		ContainerType: contracts.ContainerMihomo,
		InboundTag:    i.Tag(),
		Protocol:      i.Protocol(),
		Network:       forwardNetworkForProtocol(i.Protocol()),
	})
	if err != nil {
		return 0, fmt.Errorf("mihomo: inbound %q: get bind port for %q: %w", i.Tag(), username, err)
	}
	i.markAddedUser(username)
	return bindPort, nil
}

// RemoveUser tears down the forward rule for a user on this inbound. Always
// clears tracking (even when userMgr is nil) so a later Add will re-allocate.
// Idempotent — no-op when no rule is present.
//
// Uses GetUserPortByDstForCleanup so users that are already in "deleting"
// state (two-phase UserManager deletion) still resolve; without that the
// release step would silently skip users mid-deletion and leak the port.
func (i *MihomoInbound) RemoveUser(username string) error {
	i.unmarkAddedUser(username)
	if i.userMgr == nil {
		return nil
	}
	port, ok := i.userMgr.GetUserPortByDstForCleanup(username, i.Port())
	if !ok {
		return nil
	}
	if err := i.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
		Username: username,
		BindPort: port,
	}); err != nil {
		return fmt.Errorf("mihomo: inbound %q: release port for %q: %w", i.Tag(), username, err)
	}
	return nil
}

// ReleaseAllUserPorts releases every forward rule associated with this
// inbound tag AND clears the local addedUsers tracking set so the inbound's
// state is self-consistent afterwards. Delegates the rule cleanup to
// UserManager.ReleaseInboundPorts (atomic "clear this tag" path).
//
// Clearing addedUsers matters if the inbound lives on after release —
// future AddUser fast-paths would otherwise see hasAddedUser=true and try
// to reuse a stale PortMappings entry (ReleaseInboundPorts deliberately
// leaves PortMappings untouched; see usermanager.go:1377). Today the only
// caller is RemoveInboundConfig (inbound is about to be dropped from the
// map), but keeping the method self-consistent lets callers compose it
// safely in the future.
func (i *MihomoInbound) ReleaseAllUserPorts() error {
	if i.userMgr == nil {
		return nil
	}
	if err := i.userMgr.ReleaseInboundPorts(i.Tag()); err != nil {
		return err
	}
	i.addedUsersMu.Lock()
	clear(i.addedUsers)
	i.addedUsersMu.Unlock()
	return nil
}
