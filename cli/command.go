package main

import (
	"fmt"
	"reflect"

	"github.com/lureiny/go-prompt"
	"github.com/lureiny/v2raymg/cli/client"
)

func initPromptAndRegister() *prompt.Prompt {
	updateLocalNodeList()
	updateLocalUserList()
	m := prompt.NewPrompt(
		prompt.WithPromptPrefixOption(">>> "),
		prompt.WithSuggestNum(4),
		prompt.WithHelpMsg(),
		prompt.WithDefaultHandlerCallback(handlerCallback),
	)
	m.RegisterHandler(listNode, "ListNode")

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

	m.RegisterHandler(fastAddInbound, "FastAddInbound",
		prompt.WithSuggests([]prompt.Suggest{
			targetSuggest,
			tagSuggest,
			protocolSuggest,
			streamSuggest,
			domainSuggest,
			isXtlsSuggest,
			portSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

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

	m.RegisterHandler(deleteUser, "DeleteUser",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			userNameSuggest,
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

	m.RegisterHandler(copyUserInbound, "CopyUserInbound",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			srcTagSuggest,
			dstTagSuggest,
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

	m.RegisterHandler(addAllUserToInbound, "AddAllUserToInbound",
		prompt.WithSuggests([]prompt.Suggest{
			getSuggestWithTemplate(targetSuggest, WihtDefault("")),
			dstTagSuggest,
		}),
		prompt.WithGetSuggestMethod(GetSuggest),
	)

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

func listNode() error {
	var err error = nil
	nodeMutex.Lock()
	defer nodeMutex.Unlock()
	localNodeList, err = client.ListNode(getHost(), getToken())
	if err != nil {
		return err
	}
	for _, node := range localNodeList {
		fmt.Printf("%v\n", node)
	}
	return nil
}

func listCert(target string) error {
	certList, err := client.ListCert(getHost(), getToken(), target)
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
	result, err := client.SetGatewayModel(getHost(), getToken(), target, enableGatewayModel)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func applyCert(target, domain string) error {
	result, err := client.ApplyCert(getHost(), getToken(), target, domain)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func fastAddInbound(target, tag, protocol, stream, domain string, isXtls bool, port int) error {
	result, err := client.FastAddInbound(getHost(), getToken(), target, tag, protocol, stream, domain, isXtls, port)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func copyUserBetweenNodes(srcNode, dstNode string) error {
	result, err := client.CopyUserBetweenNodes(getHost(), getToken(), srcNode, dstNode)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func addUser(target, userName, password, tags string, expire, ttl int) error {
	result, err := client.AddUser(getHost(), getToken(), target, userName, password, tags, expire, ttl)
	if err != nil {
		return err
	}
	fmt.Printf("add user[%v] with result: %v\n", userName, result)
	return nil
}

func updateUser(target, userName, password string, expire, ttl int) error {
	result, err := client.UpdateUser(getHost(), getToken(), target, userName, password, expire, ttl)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func deleteUser(target, userName, tags string) error {
	result, err := client.DeleteUser(getHost(), getToken(), target, userName, tags)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func resetUser(target, userName string) error {
	result, err := client.ResetUser(getHost(), getToken(), target, userName)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func listUser(target string) error {
	result, err := client.ListUser(getHost(), getToken(), target)
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
	result, err := client.ClearUser(getHost(), getToken(), target, users)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func copyUserInbound(target, srcTag, dstTag string) error {
	result, err := client.CopyUserInBound(getHost(), getToken(), target, srcTag, dstTag)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func deleteInbound(target, srcTag string) error {
	result, err := client.DeleteInBound(getHost(), getToken(), target, srcTag)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func addAllUserToInbound(target, inboundTag string) error {
	result, err := client.ListUser(getHost(), getToken(), target)
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
