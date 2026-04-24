package mihomo

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
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
	SharedCred MihomoSharedCred `json:"shared_cred"`

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

// shouldCleanupCerts reports whether RemoveInboundConfig may remove the
// cert/key files on disk. True only for sources v2raymg itself writes
// ("pem" and "self_signed"); externally-managed sources are left alone
// to avoid accidentally nuking caller-maintained material or certmgr's
// renewal state.
func (c MihomoSharedCred) shouldCleanupCerts() bool {
	return c.CertSource == "pem" || c.CertSource == "self_signed"
}

// Errors surfaced by the mihomo inbound layer. Callers can use errors.Is to
// branch on these when they need to (e.g. HTTP handlers returning distinct
// status codes for "unknown protocol" vs "missing field").
var (
	// ErrProtocolNotSupported is returned when an inbound targets a protocol
	// outside the MVP trio (vmess / trojan / shadowsocks). See D4 in
	// docs/mihomo-container-implementation-plan.md.
	ErrProtocolNotSupported = errors.New("mihomo: protocol not supported (MVP: vmess/trojan/shadowsocks)")

	// ErrMissingCredential is returned when a required protocol-specific
	// credential field is empty.
	ErrMissingCredential = errors.New("mihomo: required credential missing")
)

// NewMihomoInbound creates a MihomoInbound with the loopback listen address.
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

// Validate performs structural validation plus per-protocol credential checks.
// Returns ErrProtocolNotSupported or ErrMissingCredential (wrapped with
// field-level context) on failure.
func (i *MihomoInbound) Validate() error {
	if err := i.DefaultInbound.Validate(); err != nil {
		return err
	}
	switch i.Protocol() {
	case contracts.ProtocolVMess:
		if i.SharedCred.UUID == "" {
			return fmt.Errorf("%w: vmess requires uuid", ErrMissingCredential)
		}
	case contracts.ProtocolTrojan:
		if i.SharedCred.Password == "" {
			return fmt.Errorf("%w: trojan requires password", ErrMissingCredential)
		}
		// Cert + key must be paired. mihomo itself will reject a trojan
		// listener with no cert at all (see MihomoSharedCred.CertFile
		// doc), but the mismatched-pair case is a v2raymg-layer bug we
		// can catch earlier with a clearer error.
		if (i.SharedCred.CertFile == "") != (i.SharedCred.KeyFile == "") {
			return fmt.Errorf("%w: trojan cert_file and key_file must be set together", ErrMissingCredential)
		}
	case contracts.ProtocolShadowsocks:
		if i.SharedCred.Password == "" {
			return fmt.Errorf("%w: shadowsocks requires password", ErrMissingCredential)
		}
		if i.SharedCred.Cipher == "" {
			return fmt.Errorf("%w: shadowsocks requires cipher", ErrMissingCredential)
		}
	default:
		return fmt.Errorf("%w: %q", ErrProtocolNotSupported, i.Protocol())
	}
	return nil
}

// mihomoInboundJSON is the on-disk representation written to InboundStore.
// Keep the shape backward-compatible across stages: SharedCred's omitempty
// tags already skip irrelevant fields per protocol, so vmess records don't
// carry cipher, etc.
type mihomoInboundJSON struct {
	Tag        string           `json:"tag"`
	Protocol   string           `json:"protocol"`
	Port       uint32           `json:"port"`
	ListenAddr string           `json:"listen_addr"`
	SharedCred MihomoSharedCred `json:"shared_cred"`
}

// ToNative serialises the inbound for InboundStore.NativeJSON. Overrides
// DefaultInbound.ToNative (which returns ErrInboundToNativeNotImplemented).
func (i *MihomoInbound) ToNative() ([]byte, error) {
	return json.Marshal(&mihomoInboundJSON{
		Tag:        i.Tag(),
		Protocol:   string(i.Protocol()),
		Port:       i.Port(),
		ListenAddr: i.ListenAddr(),
		SharedCred: i.SharedCred,
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
	inb := NewMihomoInbound(j.Tag, contracts.Protocol(j.Protocol), j.Port, j.SharedCred)
	if j.ListenAddr != "" {
		inb.DefaultInbound.SetListenAddr(j.ListenAddr)
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
