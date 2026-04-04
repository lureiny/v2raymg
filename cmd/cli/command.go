package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/lureiny/go-prompt"
	"github.com/lureiny/v2raymg/cmd/cli/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
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

	m.RegisterHandler(addUser, "AddUser",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNameSuggest,
			passwordSuggest,
			tagsSuggest,
			expireSuggest,
			ttlSuggest,
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
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(deleteUser, "DeleteUsers",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNamesSuggest,
			tagsSuggest,
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

	m.RegisterHandler(clearUsers, "ClearUsers",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNamesSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(addAllUserToInbound, "AddAllUserToInbound",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			dstTagSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(getUserSubUrl, "GetUserSubUrl",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("all")),
			userNameSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- Stats & Misc ---
	m.RegisterHandler(getStat, "GetStat",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("all")),
			patternSuggest,
			resetSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(updateProxy, "UpdateProxy",
		prompt.WithSuggests([]prompt.Suggest{
			targetSuggest,
			versionTagSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	m.RegisterHandler(pingTarget, "PingTarget",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			{
				Text:        "times",
				Description: "ping times",
				Default:     int(10),
			},
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
			userNameSuggest,
			{Text: "role", Description: "role: admin or normal", Default: "normal"},
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

	// --- Session lifecycle (JWT only) ---
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

func addUser(target, userName, password, tags string, expire, ttl int) error {
	result, err := client.AddUser(getHost(), getAuthToken(), target, userName, password, tags, expire, ttl)
	if err != nil {
		return err
	}
	fmt.Printf("add user[%v] with result: %v\n", userName, result)
	return nil
}

func updateUser(target, userName, password string, expire, ttl int) error {
	result, err := client.UpdateUser(getHost(), getAuthToken(), target, userName, password, expire, ttl)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func deleteUser(target, userNames, tags string) error {
	users := strings.Split(userNames, ",")
	for _, user := range users {
		result, err := client.DeleteUser(getHost(), getAuthToken(), target, user, tags)
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
		fmt.Println(node, ":")
		for _, user := range users {
			fmt.Println(user)
		}
	}
	return nil
}

func clearUsers(target, users string) error {
	result, err := client.ClearUser(getHost(), getAuthToken(), target, users)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func addAllUserToInbound(target, inboundTag string) error {
	result, err := client.ListUser(getHost(), getAuthToken(), target)
	if err != nil {
		return err
	}
	users := result[target]
	for _, user := range users {
		if err := addUser(target, user.GetName(), user.GetPasswd(), inboundTag, int(user.GetExpireTime()), 0); err != nil {
			fmt.Printf("add user[%s] to inbound[%s] fail, err: %v\n", user.GetName(), inboundTag, err)
		}
	}
	return nil
}

func getUserSubUrl(target, userName string) error {
	uri, err := url.Parse(getHost())
	if err != nil {
		uri = &url.URL{
			Host:   getHost(),
			Scheme: "http",
		}
	}
	uri.Path = "sub"
	if len(localUserList) == 0 {
		listUser(target)
	}
	var targetUser *proto.User = nil
	for _, users := range localUserList {
		for _, user := range users {
			if user.GetName() == userName {
				targetUser = user
			}
		}
	}
	if targetUser == nil {
		fmt.Printf("user[%s] not exist", userName)
		return nil
	}
	query := uri.Query()
	query.Add("target", target)
	query.Add("user", targetUser.GetName())
	query.Add("pwd", targetUser.GetPasswd())
	uri.RawQuery = query.Encode()
	fmt.Println(uri.String())
	return nil
}

// ---- Stats & Misc ----

func getStat(target, pattern string, reset bool) error {
	result, err := client.GetStat(getHost(), getAuthToken(), target, pattern, reset)
	if err != nil {
		return err
	}
	// try pretty print JSON
	var pretty interface{}
	if jsonErr := json.Unmarshal([]byte(result), &pretty); jsonErr == nil {
		b, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println(result)
	}
	return nil
}

func updateProxy(target, versionTag string) error {
	result, err := client.UpdateProxy(getHost(), getAuthToken(), target, versionTag)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func pingTarget(target string, times int) error {
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

func setUserRole(username, role string) error {
	result, err := client.SetUserRole(getHost(), getAuthToken(), username, role)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// ---- Session lifecycle (JWT only) ----

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
