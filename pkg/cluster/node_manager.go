package cluster

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// DirtyNodeTTL is how long a superseded node_id stays tombstoned.
//
// When an address turns out to be answered by a different identity, dropping
// the old entry is not enough: peers that have not noticed yet keep advertising
// it, and the add-only merge would insert it right back with a fresh CreateTime,
// buying it another NodeTimeOut. Every round of gossip would renew it. The
// tombstone makes the removal stick until the whole cluster has converged.
//
// 3x NodeTimeOut: a peer stops advertising a node once IsCompleteRegister goes
// false, which takes at most NodeTimeOut after the node stops heartbeating to
// it; the rest is margin for the reconcile backoff.
const DirtyNodeTTL int64 = 3 * NodeTimeOut

// provisionalKeyPrefix marks directory keys that are addresses rather than
// identities. A UUID never collides with it.
const provisionalKeyPrefix = "addr:"

// ProvisionalKey is the directory key for a peer whose identity is not known
// yet — a statically configured seed, a row replayed from the pre-2.8 database,
// or a node advertised by a peer too old to carry an id. The entry is upgraded
// to its real id the first time the peer identifies itself.
func ProvisionalKey(host string, port int32) string {
	return provisionalKeyPrefix + host + ":" + strconv.FormatInt(int64(port), 10)
}

// DirectoryKey returns the map key under which a node is filed: its node_id once
// known, otherwise a provisional address key.
//
// Identity — not name — is the primary key because name, host and port are all
// mutable config fields. Keying by any of them meant that editing it turned a
// node into a stranger competing with its own stale entry, which could only be
// resolved by waiting out the node timeout.
func DirectoryKey(n *Node) string {
	if id := n.GetNodeId(); id != "" {
		return id
	}
	return ProvisionalKey(n.GetHost(), n.GetPort())
}

// IsProvisionalKey reports whether key refers to an entry with no known identity.
func IsProvisionalKey(key string) bool {
	return len(key) >= len(provisionalKeyPrefix) && key[:len(provisionalKeyPrefix)] == provisionalKeyPrefix
}

type NodeManager struct {
	nodes *map[string]*Node
	// dirty maps a superseded node_id to the unix time its tombstone expires.
	// Consulted by the merge path only: a node may always re-identify itself
	// directly, since the node itself is the authority on its own existence,
	// but no third party may reintroduce it by gossip.
	dirty map[string]int64
	name  string
	// lock 保护 nodes 指针及其指向的 map, 以及 dirty。
	// 注意: 持 lock 期间不得调用本类型的其他方法(RWMutex 不可重入)。
	// 锁序: nm.lock -> node.mu, 反向禁止 (node.mu 是 leaf lock)。
	lock sync.RWMutex
}

const defaultNodeManagerName = "NodeManager"

type NodeFilter func(*Node) bool

func NewNodeManager() NodeManager {
	return NodeManager{
		nodes: &map[string]*Node{},
		dirty: map[string]int64{},
		lock:  sync.RWMutex{},
		name:  defaultNodeManagerName,
	}
}

// Add files a node under its directory key, replacing whatever was there.
func (nm *NodeManager) Add(node *Node) {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	(*nm.nodes)[DirectoryKey(node)] = node
}

// AddWithKey files a node under an explicit key. Used for the wrong-token list,
// which is a name-keyed blacklist rather than a directory.
func (nm *NodeManager) AddWithKey(key string, node *Node) {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	(*nm.nodes)[key] = node
}

// HaveNode 判断是否存在该key
func (nm *NodeManager) HaveNode(key string) bool {
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	_, ok := (*nm.nodes)[key]
	return ok
}

// Delete 删除指定key, 并关闭该条目持有的连接
func (nm *NodeManager) Delete(key string) {
	nm.lock.Lock()
	n := (*nm.nodes)[key]
	delete((*nm.nodes), key)
	nm.lock.Unlock()
	if n != nil {
		n.CloseConn()
	}
}

// SetName ...
func (nm *NodeManager) SetName(name string) {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	nm.name = name
}

// MarkDirty tombstones a node_id for DirtyNodeTTL and drops its entry.
//
// A STATIC entry is demoted rather than deleted: it is rewound to the
// provisional, address-only form LoadStaticNode produces. static_nodes is the
// only bootstrap path there is, and it is read once at startup — deleting a
// configured seed because whoever answers its address changed identity would
// silently remove the node's ability to rejoin the cluster at all, and nothing
// would ever put it back.
func (nm *NodeManager) MarkDirty(nodeID string, now int64) {
	if nodeID == "" {
		return
	}
	nm.lock.Lock()
	nm.dirty[nodeID] = now + DirtyNodeTTL
	n := (*nm.nodes)[nodeID]
	delete(*nm.nodes, nodeID)
	if n != nil && n.IsLocal() {
		seed := &Node{
			Node: &proto.Node{
				Name: n.GetName(),
				Host: n.GetHost(),
				Port: n.GetPort(),
			},
			isLocal:    true,
			CreateTime: now,
		}
		(*nm.nodes)[DirectoryKey(seed)] = seed
	}
	nm.lock.Unlock()
	if n != nil {
		n.CloseConn()
	}
}

// AddIfAbsent files a node only when nothing already occupies its directory key
// and no existing entry matches it, reporting whether it was stored.
//
// Gossip needs this rather than a Lookup followed by an Add: those are two lock
// acquisitions, and a registration landing in between would be overwritten by
// the Add — destroying the tokens and heartbeat timestamps of a peer that had
// just completed a handshake.
func (nm *NodeManager) AddIfAbsent(node *Node) bool {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	if lookupPeerLocked(nm.nodes, node.Node) != nil {
		return false
	}
	(*nm.nodes)[DirectoryKey(node)] = node
	return true
}

// DeleteNode removes an entry only if the directory still holds this exact node.
//
// Callers hold a *Node captured from an earlier snapshot, and an address, name or
// identity change replaces the entry rather than mutating it, so a key recomputed
// from a stale pointer can by then belong to a different, live node.
func (nm *NodeManager) DeleteNode(node *Node) {
	if node == nil {
		return
	}
	nm.lock.Lock()
	key := DirectoryKey(node)
	removed := (*nm.nodes)[key] == node
	if removed {
		delete(*nm.nodes, key)
	}
	nm.lock.Unlock()
	if removed {
		node.CloseConn()
	}
}

// IsDirty reports whether a node_id is currently tombstoned.
func (nm *NodeManager) IsDirty(nodeID string, now int64) bool {
	if nodeID == "" {
		return false
	}
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	exp, ok := nm.dirty[nodeID]
	return ok && exp > now
}

// LoadStaticNode 加载本地配置文件中的node
//
// Static peers are filed provisionally, by address, and carry NO name: a seed is
// an address to dial, and until the peer there answers we do not know who it is.
// The entry is upgraded to the real identity and the peer's own name (keeping
// isLocal, so it is still never evicted) as soon as that peer registers or
// answers.
func (nm *NodeManager) LoadStaticNode(nodes []StaticNode) error {
	nm.lock.Lock()
	defer nm.lock.Unlock()

	localNode := &Node{
		Node: &globalLocalNode.Node,
	}

	for _, node := range nodes {
		// 过滤掉与本地节点相同的节点
		if node.IsValide(localNode) {
			log.Info("Load Static Node",
				"manager_name", nm.name,
				"node", fmt.Sprintf("%s:%d", node.Host, node.Port),
			)
			n := &Node{
				// No name: we have an address and nothing else. The peer supplies
				// its own in the first response.
				Node: &proto.Node{
					Port: node.Port,
					Host: node.Host,
				},
				isLocal:    true,
				CreateTime: time.Now().Unix(),
			}
			(*nm.nodes)[DirectoryKey(n)] = n
		}
	}
	return nil
}

// Clear 清空nodes
func (nm *NodeManager) Clear() {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	nm.nodes = &map[string]*Node{}
}

// Get returns the node filed under key, or nil.
func (nm *NodeManager) Get(key string) *Node {
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	if n, ok := (*nm.nodes)[key]; ok {
		return n
	}
	return nil
}

// FindByName returns the entry with this name plus how many entries carry it.
//
// Since the directory is keyed by identity, a name is no longer guaranteed
// unique: two nodes may be misconfigured with the same one. That used to be a
// hard failure (the second was refused registration); now they coexist and the
// count lets callers surface the ambiguity instead of silently picking one.
func (nm *NodeManager) FindByName(name string) (*Node, int) {
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	return findByNameLocked(nm.nodes, name)
}

func findByNameLocked(nodes *map[string]*Node, name string) (*Node, int) {
	if name == "" {
		return nil, 0
	}
	var first *Node
	count := 0
	for _, n := range *nodes {
		if n.GetName() != name {
			continue
		}
		count++
		if first == nil || (!first.isSelf && n.isSelf) {
			first = n
		}
	}
	return first, count
}

// FindByAddr returns the entry dialling this address, or nil. At most one entry
// can legitimately hold a given address — it is a single listening socket.
func (nm *NodeManager) FindByAddr(host string, port int32) *Node {
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	return findByAddrLocked(nm.nodes, host, port, "")
}

// isForeignLiveRival reports whether an entry found by ADDRESS is plainly a
// different node rather than the claimant's own stale record.
//
// Address matching is what lets a rebuilt node reclaim its entry: it comes back
// with the same config and a new identity, and its predecessor still looks
// freshly registered because it only stopped heartbeating moments ago. That is
// indistinguishable from a takeover, so it is accepted.
//
// It is distinguishable from TWO DIFFERENT nodes misconfigured onto one address:
// a rebuild keeps the name (the config did not change), whereas two nodes have
// two names. Without this test each would resolve to the other, replace it and
// tombstone its id, so the pair would ping-pong the directory entry and each
// other's tombstones on every heartbeat. Treating them as separate entries
// instead leaves the collision visible (absorbAddrLocked reports the rival) and
// lets whichever one is wrong be fixed by an operator.
func isForeignLiveRival(inc *Node, claim *proto.Node) bool {
	return inc.RegisteredLocal() &&
		claim.GetNodeId() != "" && inc.GetNodeId() != "" &&
		claim.GetNodeId() != inc.GetNodeId() &&
		claim.GetName() != inc.GetName()
}

func findByAddrLocked(nodes *map[string]*Node, host string, port int32, exceptKey string) *Node {
	for key, n := range *nodes {
		if key == exceptKey || n.isSelf {
			continue
		}
		if n.GetHost() == host && n.GetPort() == port {
			return n
		}
	}
	return nil
}

// LookupPeer finds the directory entry a peer's self-description refers to.
//
// Resolution order is deliberate:
//  1. node_id — authoritative, and survives any config edit;
//  2. address — catches provisional entries (static seeds, replayed DB rows) and
//     the case where a rebuilt node reuses an address under a new identity;
//  3. name — ONLY when neither side knows an id, i.e. a pre-2.8 peer matched
//     against an entry we have never identified. Matching a known-id claim by
//     name would let a mistyped label bind a peer to an unrelated address.
func (nm *NodeManager) LookupPeer(claim *proto.Node) *Node {
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	return lookupPeerLocked(nm.nodes, claim)
}

func lookupPeerLocked(nodes *map[string]*Node, claim *proto.Node) *Node {
	if claim == nil {
		return nil
	}
	if id := claim.GetNodeId(); id != "" {
		if n, ok := (*nodes)[id]; ok {
			return n
		}
	}
	if n := findByAddrLocked(nodes, claim.GetHost(), claim.GetPort(), ""); n != nil && !isForeignLiveRival(n, claim) {
		return n
	}
	if claim.GetNodeId() == "" {
		if n, count := findByNameLocked(nodes, claim.GetName()); count == 1 && n.GetNodeId() == "" {
			return n
		}
	}
	return nil
}

// GetAllNode 返回当前节点表的浅拷贝快照, key 为目录键(node_id 或 provisional 地址键)。
// 修改返回的 map 不影响内部状态; value 仍是共享的 *Node 指针。
func (nm *NodeManager) GetAllNode() map[string]*Node {
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	nodes := make(map[string]*Node, len(*nm.nodes))
	for key, node := range *nm.nodes {
		nodes[key] = node
	}
	return nodes
}

func (nm *NodeManager) GetNodesWithFilter(filter NodeFilter) []*Node {
	nodes := []*Node{}
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	for _, n := range *nm.nodes {
		if filter(n) {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// Filter 过滤掉不符合条件的Node, 并顺带回收过期的墓碑。
//
// 单一写锁: 早先的实现先取读锁构建新 map、放锁、再取写锁替换指针, 两把锁之间发生的
// Add 会被整体覆盖而静默丢失 —— 而 Add 正是注册成功的那一刻。谓词是 leaf(只取
// node.mu), 锁序安全, 拆成两段没有任何收益。
func (nm *NodeManager) Filter(filter NodeFilter) {
	var dropped []*Node

	nm.lock.Lock()
	tmpNM := &map[string]*Node{}
	for key, node := range *nm.nodes {
		if filter(node) {
			(*tmpNM)[key] = node
		} else {
			dropped = append(dropped, node)
			log.Info("drop node",
				"manager_name", nm.name,
				"node_name", node.Name,
				"node_id", node.GetNodeId(),
				"node", fmt.Sprintf("%s:%d", node.Host, node.Port),
			)
		}
	}
	nm.nodes = tmpNM

	now := time.Now().Unix()
	for id, exp := range nm.dirty {
		if exp <= now {
			delete(nm.dirty, id)
		}
	}
	nm.lock.Unlock()

	for _, n := range dropped {
		n.CloseConn()
	}
}

// --- registration resolution ---

// ResolveOutcome classifies what a registration did to the directory.
type ResolveOutcome int

const (
	// ResolveNew: a peer we had never heard of.
	ResolveNew ResolveOutcome = iota
	// ResolveSame: a repeat registration, nothing about the peer changed.
	ResolveSame
	// ResolveMoved: same identity, new address. The routine case this whole
	// design exists for — an operator edited proxy_host or rpc_port.
	ResolveMoved
	// ResolveRenamed: same identity, new name.
	ResolveRenamed
	// ResolveReplaced: a DIFFERENT identity now answers for an entry we held.
	// Almost always a node rebuilt with a fresh database. Accepted, because a
	// refusal has no recovery path — the instance that owned the old id is gone
	// and will never come back to release it — but reported loudly, which is
	// what makes the takeover visible rather than silent.
	ResolveReplaced
)

func (o ResolveOutcome) String() string {
	switch o {
	case ResolveNew:
		return "new"
	case ResolveSame:
		return "same"
	case ResolveMoved:
		return "moved"
	case ResolveRenamed:
		return "renamed"
	case ResolveReplaced:
		return "replaced"
	}
	return "unknown"
}

// ResolveResult reports what ResolveRegistration decided.
type ResolveResult struct {
	Outcome ResolveOutcome
	// Node is the authoritative directory entry after the call. Never nil.
	Node *Node
	// Token is the in-token the peer must present from now on.
	Token string
	// Repeat is true when the peer was already registered locally and the token
	// is the one it was given before (the historical code 102 path).
	Repeat bool

	PrevName   string
	PrevHost   string
	PrevPort   int32
	PrevNodeID string

	// Absorbed lists directory keys removed because they claimed this address
	// without being able to defend it.
	Absorbed []string
	// LiveAddrRivals lists entries that claim this address AND are still
	// actively registered. They are left alone: two live nodes on one address is
	// a misconfiguration to report, not one to resolve by deleting a live peer.
	LiveAddrRivals []string
	// Tombstoned lists node_ids that were superseded and must not come back via
	// gossip until DirtyNodeTTL expires.
	Tombstoned []string
	// RetiredConns must be closed by the caller, outside the lock.
	RetiredConns []*grpc.ClientConn
}

// ResolveRegistration files a peer's self-description into the directory and
// reports what changed, doing lookup, replacement and address de-duplication
// under a single write lock.
//
// It cannot be assembled from Get/Add/Delete calls: two peers registering
// concurrently (or one retrying) would interleave between them and produce a
// lost update or a duplicate entry. For the same reason the body must not call
// the exported methods — the lock is not reentrant.
//
// The peer is the sole authority on its own name, address and identity, so this
// never refuses. Refusal was tried and is structurally wrong: the party that
// would have to withdraw a stale claim is precisely the instance that no longer
// exists. Caller-side policy (rejecting a peer that claims OUR identity, for
// instance) belongs in the handler.
//
// freshToken is minted by the caller so no entropy is drawn under the lock.
func (nm *NodeManager) ResolveRegistration(claim *proto.Node, freshToken string, now int64) ResolveResult {
	res := ResolveResult{Token: freshToken}
	if claim == nil {
		return res
	}

	nm.lock.Lock()
	defer nm.lock.Unlock()

	inc := lookupPeerLocked(nm.nodes, claim)

	if inc == nil {
		n := nodeFromClaim(claim, freshToken, now, false, now)
		nm.absorbAddrLocked(&res, claim.GetHost(), claim.GetPort(), DirectoryKey(n))
		(*nm.nodes)[DirectoryKey(n)] = n
		res.Outcome, res.Node = ResolveNew, n
		nm.clearDirtyLocked(n.GetNodeId())
		return res
	}

	res.PrevName, res.PrevHost, res.PrevPort = inc.GetName(), inc.GetHost(), inc.GetPort()
	res.PrevNodeID = inc.GetNodeId()

	addrChanged := inc.GetHost() != claim.GetHost() || inc.GetPort() != claim.GetPort()
	nameChanged := inc.GetName() != claim.GetName()
	// An id is only "changed" when both sides actually have one. An empty id on
	// either side is the normal state for a static seed, a replayed database row
	// or a pre-2.8 peer — never evidence of a different node.
	idChanged := claim.GetNodeId() != "" && inc.GetNodeId() != "" && claim.GetNodeId() != inc.GetNodeId()
	idLearned := claim.GetNodeId() != "" && inc.GetNodeId() == ""

	// Nothing identity-bearing changed. Note this is also the path an id-less
	// (pre-2.8) repeat from an already-identified peer takes: such a claim can
	// only have matched by ADDRESS, and with the same name and address there is
	// nothing to apply — so an identity we already learned is preserved here by
	// construction, without any special case, and a single legacy exchange cannot
	// strip identity off the directory.
	if !addrChanged && !nameChanged && !idChanged && !idLearned {
		if inc.RegisteredLocal() {
			res.Outcome, res.Token, res.Repeat = ResolveSame, inc.GetInToken(), true
		} else {
			inc.SetInToken(freshToken)
			res.Outcome = ResolveSame
		}
		inc.SetRecvHeartBeatTime(now)
		res.Node = inc
		nm.clearDirtyLocked(inc.GetNodeId())
		return res
	}

	// Something identity-bearing changed. Build a replacement rather than
	// writing the live node's fields: see Node's concurrency contract, and note
	// that a cached grpc.ClientConn is pinned to the address it was dialled
	// with, so keeping the old node would keep reaching the old address.
	// The replacement deliberately does NOT inherit the incumbent's node_id when
	// the claim carries none. Reaching here with an empty claim id means the
	// incumbent was matched by ADDRESS and its name differs — which is exactly
	// what a pre-2.8 node taking over a decommissioned address looks like.
	// Adopting the departed node's identity there would file the newcomer under
	// it, persist it under it, and make every later message from the real owner
	// look like a takeover. Dropping an id is recoverable: the entry falls back to
	// its address key and re-identifies on the next identified exchange.
	repl := nodeFromClaim(claim, freshToken, now, inc.IsLocal(), inc.CreateTime)
	if addrChanged || idChanged {
		// The session belonged to a DIFFERENT process, so none of it survives.
		// That is obvious for a moved peer — the out-token was issued by whatever
		// answers the old address, and a cached connection is pinned to it because
		// grpc.Dial fixes its target at dial time. It is equally true for an
		// identity change at the SAME address: a new identity is precisely what a
		// rebuilt process looks like, so the token its predecessor issued us is
		// dead and its connection points at a socket nobody is holding any more.
		if conn := inc.takeConn(); conn != nil {
			res.RetiredConns = append(res.RetiredConns, conn)
		}
	} else {
		// Same socket AND same identity: the peer merely renamed itself, or told us
		// an id for the first time. Tearing the session down for that would make
		// every peer in the cluster re-register once during a rolling upgrade —
		// dropping out of each other's advertised sets and churning the digest —
		// purely to record a field that changes nothing about reachability.
		inc.handOffRuntimeTo(repl)
		repl.inToken = freshToken
		repl.recvHeartBeatTime = now
	}
	delete(*nm.nodes, DirectoryKey(inc))

	switch {
	case idChanged:
		res.Outcome = ResolveReplaced
		nm.tombstoneLocked(&res, inc.GetNodeId(), now)
	case addrChanged:
		res.Outcome = ResolveMoved
	case nameChanged:
		res.Outcome = ResolveRenamed
	default:
		res.Outcome = ResolveSame // pure id learn
	}

	newKey := DirectoryKey(repl)
	nm.absorbAddrLocked(&res, claim.GetHost(), claim.GetPort(), newKey)
	(*nm.nodes)[newKey] = repl
	res.Node = repl
	nm.clearDirtyLocked(repl.GetNodeId())
	return res
}

// AdoptIdentity re-files the entry at (host, port) under the identity that
// address just reported, carrying its session across unchanged.
//
// This is the OUTBOUND counterpart to ResolveRegistration: when we dial a static
// seed or a replayed database row, the response tells us who actually answered,
// and holding that entry provisionally any longer would mean re-learning the
// same peer as a second entry when it registers back to us.
//
// It never touches the heartbeat timestamps, so an entry adopted this way still
// looks un-registered until a real handshake completes. Stamping them here would
// be the classic trap: IsCompleteRegister verifies no token, so a peer we have
// merely phoned would be advertised to the cluster as authenticated.
//
// Returns the entry now on file and whether anything changed.
func (nm *NodeManager) AdoptIdentity(host string, port int32, nodeID, name string) (*Node, bool) {
	if nodeID == "" {
		return nil, false
	}
	nm.lock.Lock()

	inc := findByAddrLocked(nm.nodes, host, port, "")
	if inc == nil || inc.GetNodeId() != "" {
		nm.lock.Unlock()
		return inc, false
	}

	repl := &Node{
		Node: &proto.Node{
			Name:   pickName(name, inc.GetName()),
			Host:   inc.GetHost(),
			Port:   inc.GetPort(),
			NodeId: nodeID,
		},
		isLocal:    inc.IsLocal(),
		CreateTime: inc.CreateTime,
	}
	inc.handOffRuntimeTo(repl)

	delete(*nm.nodes, DirectoryKey(inc))
	var swept ResolveResult
	nm.absorbAddrLocked(&swept, host, port, nodeID)
	(*nm.nodes)[nodeID] = repl
	delete(nm.dirty, nodeID)
	nm.lock.Unlock()

	// Outside the lock: closing is a network operation and would block every
	// other directory reader. Dropping these on the floor instead would leak one
	// connection per resolved duplicate.
	for _, conn := range swept.RetiredConns {
		_ = conn.Close()
	}
	return repl, true
}

// RefreshName records a new name for an already-identified peer, keeping its
// session and its directory key.
//
// A rename used to reach a peer only through that peer's own re-registration,
// which happens when it restarts. Anyone who had merely learned it by gossip and
// never handshaked with it kept the old label indefinitely — so ?target=<name>
// worked on some nodes and not others. Every response carries the responder's
// current name, so the outbound path can close that gap; it just has to act on
// it rather than only reading it the first time.
//
// The key does not change (it is the identity), and neither does the address, so
// this is a same-process update: the session carries over untouched. Callers
// should only reach here when the name actually differs — a rename is rare, and
// replacing the entry on every heartbeat would be absurd.
func (nm *NodeManager) RefreshName(nodeID, name string) (*Node, bool) {
	if nodeID == "" || name == "" {
		return nil, false
	}
	nm.lock.Lock()
	defer nm.lock.Unlock()

	inc := (*nm.nodes)[nodeID]
	if inc == nil || inc.GetName() == name || inc.isSelf {
		return inc, false
	}

	repl := &Node{
		Node: &proto.Node{
			Name:   name,
			Host:   inc.GetHost(),
			Port:   inc.GetPort(),
			NodeId: nodeID,
		},
		isLocal:    inc.IsLocal(),
		CreateTime: inc.CreateTime,
	}
	inc.handOffRuntimeTo(repl)
	(*nm.nodes)[nodeID] = repl
	return repl, true
}

// pickName prefers the name the peer reports over whatever the entry already
// holds. A static seed holds nothing, so the reported name simply wins; a row
// replayed from the database holds the name that peer used last time, which is
// the right fallback if a response somehow arrives without one.
func pickName(reported, configured string) string {
	if reported != "" {
		return reported
	}
	return configured
}

// absorbAddrLocked removes other entries claiming (host, port).
//
// The registering peer just proved it holds that address over a live,
// cluster-authenticated connection, so anything else filed there is either a
// provisional placeholder (a static seed whose configured name was wrong, a
// replayed database row) or a superseded identity. An entry that is itself
// still actively registered is NOT removed: two live nodes on one address is a
// misconfiguration worth shouting about, but deleting a peer that is
// demonstrably talking to us would be worse than leaving the duplicate.
func (nm *NodeManager) absorbAddrLocked(res *ResolveResult, host string, port int32, keepKey string) {
	for key, other := range *nm.nodes {
		if key == keepKey || other.isSelf {
			continue
		}
		if other.GetHost() != host || other.GetPort() != port {
			continue
		}
		if other.RegisteredLocal() {
			res.LiveAddrRivals = append(res.LiveAddrRivals, key)
			continue
		}
		if conn := other.takeConn(); conn != nil {
			res.RetiredConns = append(res.RetiredConns, conn)
		}
		delete(*nm.nodes, key)
		res.Absorbed = append(res.Absorbed, key)
		nm.tombstoneLocked(res, other.GetNodeId(), time.Now().Unix())
	}
}

func (nm *NodeManager) tombstoneLocked(res *ResolveResult, nodeID string, now int64) {
	if nodeID == "" {
		return
	}
	nm.dirty[nodeID] = now + DirtyNodeTTL
	res.Tombstoned = append(res.Tombstoned, nodeID)
}

// clearDirtyLocked lifts a tombstone. A node that identifies itself directly is
// authoritative about its own existence, so a stale tombstone must never lock it
// out; only gossip is held back by the dirty set.
func (nm *NodeManager) clearDirtyLocked(nodeID string) {
	if nodeID != "" {
		delete(nm.dirty, nodeID)
	}
}

// nodeFromClaim builds a directory entry from a peer's self-description.
//
// outToken and reportHeartBeatTime are deliberately zero even when replacing an
// existing entry: the old out-token was issued by whatever answered the OLD
// address, so carrying it over would authenticate us to the wrong process.
func nodeFromClaim(claim *proto.Node, inToken string, now int64, isLocal bool, createTime int64) *Node {
	n := &Node{
		Node: &proto.Node{
			Name:   claim.GetName(),
			Host:   claim.GetHost(),
			Port:   claim.GetPort(),
			NodeId: claim.GetNodeId(),
		},
		isLocal:    isLocal,
		CreateTime: createTime,
	}
	n.inToken = inToken
	n.recvHeartBeatTime = now
	return n
}
