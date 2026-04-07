package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"time"

	"github.com/lureiny/go-prompt"
	"github.com/lureiny/v2raymg/cmd/cli/client"
)

func InitPromptAndRegister() *prompt.Prompt {
	updateLocalNodeList()
	updateLocalUserList()
	updateLocalInboundList()
	m := prompt.NewPrompt(
		prompt.WithPromptPrefixOption(">>> "),
		prompt.WithSuggestNum(4),
		prompt.WithHelpMsg(),
		prompt.WithDefaultHandlerCallback(handlerCallback),
	)

	// --- Node ---
	m.RegisterHandler(listNode, "ListNode")

	// --- Cert ---
	m.RegisterHandler(listCert, "ListCert",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("all")),
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(setGatewayModel, "SetGatewayModel",
		prompt.WithSuggests([]prompt.Suggest{
			targetSuggest,
			enableGatewayModelSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(applyCert, "ApplyCert",
		prompt.WithSuggests([]prompt.Suggest{
			targetSuggest,
			domainSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- Inbound ---
	m.RegisterHandler(fastAddInbound, "FastAddInbound",
		prompt.WithSuggests([]prompt.Suggest{
			targetSuggest,
			tagSuggest,
			protocolSuggest,
			streamSuggest,
			domainSuggest,
			containerSuggest,
			isXtlsSuggest,
			portSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(addInbound, "AddInbound",
		prompt.WithSuggests([]prompt.Suggest{
			targetSuggest,
			boundRawStringSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(deleteInbound, "DeleteInbound",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			srcTagSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(getInbound, "GetInbound",
		prompt.WithSuggests([]prompt.Suggest{
			targetSuggest,
			srcTagSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(listInbounds, "ListInbounds",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("all")),
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(deleteInboundByName, "DeleteInboundByName",
		prompt.WithSuggests([]prompt.Suggest{
			targetSuggest,
			containerSuggest,
			tagSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- User ---
	m.RegisterHandler(copyUserBetweenNodes, "CopyUserBetweenNodes",
		prompt.WithSuggests([]prompt.Suggest{
			srcNodeSuggest,
			dstNodeSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(listUser, "ListUser",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("all")),
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(listAllUsers, "ListAllUsers",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("all")),
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(addUser, "AddUser",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNameSuggest,
			passwordSuggest,
			expireSuggest,
			ttlSuggest,
			{Text: "role", Description: "role: admin or normal", Default: ""},
			{Text: "group", Description: "cluster group", Default: ""},
			{Text: "upload_bps", Description: "upload limit bytes/sec (0=unlimited)", Default: int64(0)},
			{Text: "download_bps", Description: "download limit bytes/sec (0=unlimited)", Default: int64(0)},
			{Text: "max_clients", Description: "max client IPs (0=unlimited)", Default: int(0)},
			{Text: "recycle_delay_sec", Description: "client recycle delay sec", Default: int(0)},
			{Text: "drain_sec", Description: "client drain sec", Default: int(0)},
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(updateUser, "UpdateUser",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNameSuggest,
			passwordSuggest,
			expireSuggest,
			ttlSuggest,
			{Text: "role", Description: "role: admin or normal (empty=no change)", Default: ""},
			{Text: "upload_bps", Description: "upload limit bytes/sec (0=no change)", Default: int64(0)},
			{Text: "download_bps", Description: "download limit bytes/sec (0=no change)", Default: int64(0)},
			{Text: "max_clients", Description: "max client IPs (0=no change)", Default: int(0)},
			{Text: "recycle_delay_sec", Description: "client recycle delay sec (0=no change)", Default: int(0)},
			{Text: "drain_sec", Description: "client drain sec (0=no change)", Default: int(0)},
			{Text: "group", Description: "cluster group (empty=no change)", Default: ""},
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(deleteUser, "DeleteUsers",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNamesSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(resetUser, "ResetUser",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNameSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- Stats & Misc ---
	m.RegisterHandler(updateProxy, "UpdateProxy",
		prompt.WithSuggests([]prompt.Suggest{
			targetSuggest,
			versionTagSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(setPingCheck, "SetPingCheck",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			enablePingCheckSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- Port rotation (JWT auth via AuthMiddleware) ---
	m.RegisterHandler(rotatePort, "RotatePort",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(userNameSuggest, WihtDefault("")),
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(rotateInboundPort, "RotateInboundPort",
		prompt.WithSuggests([]prompt.Suggest{
			containerSuggest,
			{Text: "inbound", Description: "inbound tag", Default: ""},
			{Text: "port", Description: "new listen port (0 = auto)", Default: int(0)},
			getSuggestWithTemplate(userNameSuggest, WihtDefault("")),
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(rotateAllPorts, "RotateAllPorts",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(userNameSuggest, WihtDefault("")),
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- User role management (admin only) ---
	m.RegisterHandler(setUserRole, "SetUserRole",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNameSuggest,
			{Text: "role", Description: "role: admin or normal", Default: "normal"},
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- Bandwidth / client limit management (admin only) ---
	m.RegisterHandler(setUserBandwidth, "SetUserBandwidth",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNameSuggest,
			{Text: "upload_bps", Description: "upload limit bytes/sec (0=unlimited, -1=skip)", Default: int64(-1)},
			{Text: "download_bps", Description: "download limit bytes/sec (0=unlimited, -1=skip)", Default: int64(-1)},
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(setUserClientLimit, "SetUserClientLimit",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNameSuggest,
			{Text: "max_clients", Description: "max unique client IPs (0=unlimited)", Default: int(0)},
			{Text: "recycle_delay_sec", Description: "idle slot recycle delay (-1=no change)", Default: int(-1)},
			{Text: "drain_sec", Description: "drain timeout after EOF (-1=no change)", Default: int(-1)},
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- Status ---
	m.RegisterHandler(getStatus, "GetStatus",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("all")),
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- Cert transfer ---
	m.RegisterHandler(transferCert, "TransferCert",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			domainSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- Session lifecycle (JWT only) ---
	m.RegisterHandler(changePassword, "ChangePassword",
		prompt.WithSuggests([]prompt.Suggest{
			oldPasswordSuggest,
			newPasswordSuggest,
		}),
	)
	m.RegisterHandler(logout, "Logout")
	m.RegisterHandler(profile, "Profile")

	return m
}

func handlerCallback(results []reflect.Value) {
	if len(results) == 0 {
		return
	}
	if results[0].Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		if !results[0].IsNil() {
			fmt.Printf("operation failed, err: %v\n", results[0])
			return
		}
	}
}

// ---- Node ----

func listNode() error {
	var err error = nil
	nodeMutex.Lock()
	defer nodeMutex.Unlock()
	localNodeList, err = client.ListNode(getHost(), getAuthToken())
	if err != nil {
		return err
	}
	for _, node := range localNodeList {
		fmt.Printf("%v\n", node)
	}
	return nil
}

// ---- Cert ----

func listCert(target string) error {
	certList, err := client.ListCert(getHost(), getAuthToken(), target)
	if err != nil {
		return err
	}
	for nodeName, certs := range certList {
		fmt.Printf("node[%s]:\n", nodeName)
		for _, cert := range certs {
			fmt.Printf("%v\n", cert)
		}
	}
	return nil
}

func setGatewayModel(target string, enableGatewayModel bool) error {
	result, err := client.SetGatewayModel(getHost(), getAuthToken(), target, enableGatewayModel)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func applyCert(target, domain string) error {
	result, err := client.ApplyCert(getHost(), getAuthToken(), target, domain)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// ---- Inbound ----

func fastAddInbound(target, tag, protocol, stream, domain, container string, isXtls bool, port int) error {
	node := getNode(target)
	if domain == "" && node != nil {
		domain = node.GetHost()
	}
	if tag == "" {
		h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		tag = fmt.Sprintf("%s-%s", protocol, hex.EncodeToString(h[:])[:8])
	}
	if port == 0 {
		rand.Seed(time.Now().UnixNano())
		port = 10000 + rand.Intn(40000)
	}
	result, err := client.FastAddInbound(getHost(), getAuthToken(), target, tag, protocol, stream, domain, container, isXtls, port)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func addInbound(target, boundRawString string) error {
	result, err := client.AddInBound(getHost(), getAuthToken(), target, boundRawString)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func deleteInbound(target, srcTag string) error {
	result, err := client.DeleteInBound(getHost(), getAuthToken(), target, srcTag)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func getInbound(target, srcTag string) error {
	result, err := client.GetInBound(getHost(), getAuthToken(), target, srcTag)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func listInbounds(target string) error {
	result, err := client.ListInboundsStructured(getHost(), getAuthToken(), target)
	if err != nil {
		return err
	}
	for node, inbounds := range result {
		fmt.Printf("%s :\n", node)
		for _, inb := range inbounds {
			fmt.Printf("  [%s] %s\n", inb.GetContainer(), inb.GetName())
		}
	}
	return nil
}

func deleteInboundByName(target, containerType, name string) error {
	result, err := client.DeleteInboundByName(getHost(), getAuthToken(), target, containerType, name)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// ---- User ----

func copyUserBetweenNodes(srcNode, dstNode string) error {
	result, err := client.CopyUserBetweenNodes(getHost(), getAuthToken(), srcNode, dstNode)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func addUser(target, userName, password string, expire, ttl int,
	role, group string, uploadBps, downloadBps int64, maxClients, recycleDelaySec, drainSec int) error {
	result, err := client.AddUser(getHost(), getAuthToken(), target, userName, password, expire, ttl,
		role, group, uploadBps, downloadBps, maxClients, recycleDelaySec, drainSec)
	if err != nil {
		return err
	}
	fmt.Printf("add user[%v] with result: %v\n", userName, result)
	return nil
}

func updateUser(target, userName, password string, expire, ttl int,
	role string, uploadBps, downloadBps int64, maxClients, recycleDelaySec, drainSec int, group string) error {
	result, err := client.UpdateUser(getHost(), getAuthToken(), target, userName, password, expire, ttl,
		role, uploadBps, downloadBps, maxClients, recycleDelaySec, drainSec, group)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func deleteUser(target, userNames string) error {
	users := strings.Split(userNames, ",")
	for _, user := range users {
		result, err := client.DeleteUser(getHost(), getAuthToken(), target, user)
		if err != nil {
			fmt.Printf("delete user[%s] fail > %v\n", user, err)
		} else {
			fmt.Printf("delete user[%s] succ: %v\n", user, result)
		}
	}
	return nil
}

func resetUser(target, userName string) error {
	result, err := client.ResetUser(getHost(), getAuthToken(), target, userName)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func listUser(target string) error {
	result, err := client.ListUser(getHost(), getAuthToken(), target)
	if err != nil {
		return err
	}
	userMutex.Lock()
	defer userMutex.Unlock()
	for node, users := range result {
		localUserList[node] = users
		fmt.Printf("=== %s ===\n", node)
		for _, u := range users {
			expireStr := "never"
			if u.GetExpireTime() > 0 {
				expireStr = time.Unix(u.GetExpireTime(), 0).Format("2006-01-02 15:04:05")
			}
			fmt.Printf("  %-16s role=%-8s group=%-10s expire=%s\n", u.GetName(), u.GetRole(), u.GetGroup(), expireStr)
			fmt.Printf("    auth_token=%s\n", u.GetAuthToken())
			fmt.Printf("    traffic   up=%-12d down=%-12d\n", u.GetUplink(), u.GetDownlink())
			fmt.Printf("    bandwidth upload_bps=%-10d download_bps=%-10d\n", u.GetUploadBps(), u.GetDownloadBps())
			fmt.Printf("    clients   max=%-4d recycle_delay=%ds drain=%ds\n",
				u.GetMaxClients(), u.GetClientRecycleDelaySec(), u.GetClientDrainSec())
		}
	}
	return nil
}

func listAllUsers(target string) error {
	result, err := client.ListAllUsers(getHost(), getAuthToken(), target)
	if err != nil {
		return err
	}
	for node, users := range result {
		fmt.Printf("=== %s ===\n", node)
		for _, u := range users {
			expireStr := "never"
			if u.GetExpireTime() > 0 {
				expireStr = time.Unix(u.GetExpireTime(), 0).Format("2006-01-02 15:04:05")
			}
			fmt.Printf("  %-16s role=%-8s group=%-10s expire=%s\n", u.GetName(), u.GetRole(), u.GetGroup(), expireStr)
			fmt.Printf("    auth_token=%s\n", u.GetAuthToken())
			fmt.Printf("    traffic   up=%-12d down=%-12d\n", u.GetUplink(), u.GetDownlink())
			fmt.Printf("    bandwidth upload_bps=%-10d download_bps=%-10d\n", u.GetUploadBps(), u.GetDownloadBps())
			fmt.Printf("    clients   max=%-4d recycle_delay=%ds drain=%ds\n",
				u.GetMaxClients(), u.GetClientRecycleDelaySec(), u.GetClientDrainSec())
		}
	}
	return nil
}

// ---- Stats & Misc ----

func updateProxy(target, versionTag string) error {
	result, err := client.UpdateProxy(getHost(), getAuthToken(), target, versionTag)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func setPingCheck(target string, enable bool) error {
	result, err := client.SetPingCheck(getHost(), getAuthToken(), target, enable)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// ---- Port rotation (JWT auth via AuthMiddleware) ----
// username is optional: empty = operate on own account (normal JWT); non-empty = admin targeting another user.

func rotatePort(username string) error {
	result, err := client.RotatePort(getHost(), getAuthToken(), username)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func rotateInboundPort(container, inbound string, port int, username string) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535")
	}
	result, err := client.RotateInboundPort(getHost(), getAuthToken(), username, container, inbound, port)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func rotateAllPorts(username string) error {
	result, err := client.RotateAllPorts(getHost(), getAuthToken(), username)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// ---- User role management (admin only) ----

func setUserRole(target, username, role string) error {
	result, err := client.SetUserRole(getHost(), getAuthToken(), target, username, role)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// ---- Bandwidth / client limit management (admin only) ----

func setUserBandwidth(target, username string, uploadBps, downloadBps int64) error {
	if uploadBps < -1 || downloadBps < -1 {
		return fmt.Errorf("bandwidth values must be >= 0 (set limit) or -1 (skip)")
	}
	var up, down *int64
	if uploadBps >= 0 {
		up = &uploadBps
	}
	if downloadBps >= 0 {
		down = &downloadBps
	}
	if up == nil && down == nil {
		return fmt.Errorf("at least one of upload_bps or download_bps must be specified (>= 0)")
	}
	result, err := client.SetUserBandwidth(getHost(), getAuthToken(), target, username, up, down)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func setUserClientLimit(target, username string, maxClients, recycleDelaySec, drainSec int) error {
	if maxClients < 0 {
		return fmt.Errorf("max_clients must be >= 0")
	}
	if recycleDelaySec < -1 || drainSec < -1 {
		return fmt.Errorf("recycle_delay_sec and drain_sec must be >= 0 or -1 (no change)")
	}
	var recycle, drain *int
	if recycleDelaySec >= 0 {
		recycle = &recycleDelaySec
	}
	if drainSec >= 0 {
		drain = &drainSec
	}
	result, err := client.SetUserClientLimit(getHost(), getAuthToken(), target, username, maxClients, recycle, drain)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// ---- Cert transfer ----

func transferCert(target, domain string) error {
	result, err := client.TransferCert(getHost(), getAuthToken(), target, domain)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// ---- Session lifecycle (JWT only) ----

// changePassword changes the authenticated user's own password.
func changePassword(oldPassword, newPassword string) error {
	result, err := client.ChangePassword(getHost(), getAuthToken(), oldPassword, newPassword)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// logout revokes the current JWT token and clears the local JWT cache.
// Requires JWT auth; X-Token is not accepted by the server.
func logout() error {
	token := getAuthToken()
	result, err := client.Logout(getHost(), token)
	if err != nil {
		return err
	}
	// Invalidate the cached JWT so subsequent commands trigger a fresh login.
	ClearJWTCache()
	fmt.Println(result)
	return nil
}

// profile prints the current user's profile information.
// Requires JWT auth; X-Token is not accepted by the server.
func profile() error {
	result, err := client.Profile(getHost(), getAuthToken())
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// ---- Status ----

func getStatus(target string) error {
	result, err := client.GetStatus(getHost(), getAuthToken(), target)
	if err != nil {
		return err
	}

	var data struct {
		Nodes []struct {
			NodeName           string  `json:"node_name"`
			OnlyGateway        bool    `json:"only_gateway"`
			ClusterUserEnabled bool    `json:"cluster_user_enabled"`
			MemTotal           uint64  `json:"mem_total"`
			MemUsed            uint64  `json:"mem_used"`
			MemAvailable       uint64  `json:"mem_available"`
			NumGoroutine       int32   `json:"num_goroutine"`
			CpuPercent         float64 `json:"cpu_percent"`
			NetRxBytes         uint64  `json:"net_rx_bytes"`
			NetTxBytes         uint64  `json:"net_tx_bytes"`
			TcpConnections     int32   `json:"tcp_connections"`
			NetRxSpeed         float64 `json:"net_rx_speed"`
			NetTxSpeed         float64 `json:"net_tx_speed"`
		} `json:"nodes"`
		Failed map[string]string `json:"failed"`
	}

	if err := json.Unmarshal([]byte(result), &data); err != nil {
		// fallback: print raw
		fmt.Println(result)
		return nil
	}

	for i, n := range data.Nodes {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("=== %s ===\n", n.NodeName)
		fmt.Printf("  %-22s %s\n", "Gateway Mode:", formatBool(n.OnlyGateway))
		fmt.Printf("  %-22s %s\n", "Cluster User:", formatBool(n.ClusterUserEnabled))
		fmt.Printf("  %-22s %.1f%%\n", "CPU:", n.CpuPercent)
		fmt.Printf("  %-22s %s / %s (available %s)\n", "Memory:",
			formatBytes(n.MemUsed), formatBytes(n.MemTotal), formatBytes(n.MemAvailable))
		fmt.Printf("  %-22s %d\n", "Goroutines:", n.NumGoroutine)
		fmt.Printf("  %-22s RX %s / TX %s\n", "Network:",
			formatBytes(n.NetRxBytes), formatBytes(n.NetTxBytes))
		fmt.Printf("  %-22s RX %s/s / TX %s/s\n", "Network Speed:",
			formatSpeed(n.NetRxSpeed), formatSpeed(n.NetTxSpeed))
		fmt.Printf("  %-22s %d\n", "TCP Connections:", n.TcpConnections)
	}

	if len(data.Failed) > 0 {
		fmt.Println()
		fmt.Println("=== Failed Nodes ===")
		for name, msg := range data.Failed {
			fmt.Printf("  %s: %s\n", name, msg)
		}
	}

	return nil
}

func formatBytes(b uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatSpeed(bytesPerSec float64) string {
	if bytesPerSec < 0 {
		return "N/A"
	}
	return formatBytes(uint64(bytesPerSec))
}

func formatBool(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}
