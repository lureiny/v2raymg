package main

import (
	"strings"

	"github.com/lureiny/go-prompt"
)

// suggest

var (
	targetSuggest = prompt.Suggest{
		Text:        "target",
		Description: "node name",
		Default:     "",
	}

	enableGatewayModelSuggest = prompt.Suggest{
		Text:        "enable_gateway_model",
		Description: "gateway model node will not provide proxy",
		Default:     false,
	}

	tagSuggest = prompt.Suggest{
		Text:        "tag",
		Description: "inbound tag",
		Default:     "",
	}

	protocolSuggest = prompt.Suggest{
		Text:        "protocol",
		Description: "vless, vmess or trojan",
		Default:     "trojan",
	}

	streamSuggest = prompt.Suggest{
		Text:        "stream",
		Description: "transport layer protocol",
		Default:     "tcp",
	}

	domainSuggest = prompt.Suggest{
		Text:        "domain",
		Description: "domain name",
		Default:     "",
	}

	isXtlsSuggest = prompt.Suggest{
		Text:        "is_xtls",
		Description: "is xtls",
		Default:     false,
	}

	portSuggest = prompt.Suggest{
		Text:        "port",
		Description: "inbound port",
		Default:     int(0),
	}

	srcNodeSuggest = prompt.Suggest{
		Text:        "src_node",
		Description: "src node name",
		Default:     "",
	}
	dstNodeSuggest = prompt.Suggest{
		Text:        "dst_node",
		Description: "dst node name",
		Default:     "",
	}

	userNameSuggest = prompt.Suggest{
		Text:        "user",
		Description: "user name",
		Default:     "",
	}

	userNamesSuggest = prompt.Suggest{
		Text:        "users",
		Description: "users name, eg: user1,user2,...,userN",
		Default:     "",
	}

	passwordSuggest = prompt.Suggest{
		Text:        "password",
		Description: "user password",
		Default:     "",
	}

	tagsSuggest = prompt.Suggest{
		Text:        "tags",
		Description: "xray/v2ray inbound tags, eg: tag1,tag2",
		Default:     "",
	}

	expireSuggest = prompt.Suggest{
		Text:        "expire",
		Description: "user expire time, timestamp, 0 no expire",
		Default:     int(0),
	}

	ttlSuggest = prompt.Suggest{
		Text:        "ttl",
		Description: "use to clac user expire time, uesr expire time = ttl + current time",
		Default:     int(0),
	}

	srcTagSuggest = prompt.Suggest{
		Text:        "src_tag",
		Description: "src inbound tag",
		Default:     "",
	}

	dstTagSuggest = prompt.Suggest{
		Text:        "dst_tag",
		Description: "dst inbound tag",
		Default:     "",
	}
)

type SetSuggestOption func(*prompt.Suggest)

func getSuggestWithTemplate(suggestTemplate prompt.Suggest, opts ...SetSuggestOption) prompt.Suggest {
	newSuggest := prompt.Suggest{
		Text: suggestTemplate.Text,
	}
	for _, opt := range opts {
		opt(&newSuggest)
	}
	return newSuggest
}

func WihtDefault(d interface{}) SetSuggestOption {
	return func(s *prompt.Suggest) {
		s.Default = d
	}
}

func WihtDescription(description string) SetSuggestOption {
	return func(s *prompt.Suggest) {
		s.Description = description
	}
}

func GetSuggest(h *prompt.HandlerInfo, input string) ([]prompt.Suggest, error) {
	suggests, err := prompt.DefaultGetHandlerSuggests(h, input)
	if err != nil || len(suggests) != 0 {
		return suggests, err
	}
	splitedInput := strings.Split(input, " ")
	// filter extra spaces
	inputs := []string{}
	for _, s := range splitedInput {
		if len(s) > 0 {
			inputs = append(inputs, s)
		}
	}

	isInputLast := len(input) == 0 || input[len(input)-1] != ' '
	if len(input) == 0 || !isInputLast {
		inputs = append(inputs, "") // 添加空字符串表示当前在等待输入一个新的参数, inputs的最后一个一定是当前在输入的值
	}
	// input custom param, not need suggest
	notInputHandler := len(inputs) > 1
	isInputParamValue := notInputHandler &&
		(prompt.IsBoolSuggest(h.Suggests, inputs[len(inputs)-1], h.SuggestPrefix) ||
			prompt.IsInputNotBoolValue(inputs, h.SuggestPrefix, h.Suggests))
	if isInputParamValue {
		if isInputNodeName(inputs[len(inputs)-2], h.SuggestPrefix) {
			return getNodeSuggest(inputs[len(inputs)-1])
		}

		if isInputUserName(inputs[len(inputs)-2], h.SuggestPrefix, inputs[0]) {
			return getUserSuggest(getTargetParam(input), inputs[len(inputs)-1])
		}

	}

	return []prompt.Suggest{}, nil
}

func isInputNodeName(lastFlag, suggestPrefix string) bool {
	return lastFlag == suggestPrefix+srcNodeSuggest.Text ||
		lastFlag == suggestPrefix+dstNodeSuggest.Text ||
		lastFlag == suggestPrefix+targetSuggest.Text
}

func isInputUserName(lastFlag, suggestPrefix, handlerName string) bool {
	return (lastFlag == suggestPrefix+userNameSuggest.Text ||
		lastFlag == suggestPrefix+userNamesSuggest.Text) &&
		handlerName != "AddUser"
}

// return target name
func getTargetParam(input string) string {
	splitedInput := strings.Split(input, " ")
	// filter extra spaces
	inputs := []string{}
	for _, s := range splitedInput {
		if len(s) > 0 {
			inputs = append(inputs, s)
		}
	}

	for index, i := range inputs {
		if i == "-target" && index != len(inputs)-1 {
			return inputs[index+1]
		}
	}

	return ""
}

func getUserSuggest(node, currentInput string) ([]prompt.Suggest, error) {
	userMutex.Lock()
	defer userMutex.Unlock()
	suggests := []prompt.Suggest{}

	if users, ok := localUserList[node]; !ok {
		userMap := map[string]bool{}
		for _, userList := range localUserList {
			for _, u := range userList {
				userMap[u.GetName()] = true

			}
		}
		for u := range userMap {
			if prompt.IsMatch(currentInput, u) {
				suggests = append(suggests, prompt.Suggest{
					Text:        u,
					SuggestType: prompt.SuggestOfHandler,
				})
			}
		}
	} else {
		for _, u := range users {
			if prompt.IsMatch(currentInput, u.GetName()) {
				suggests = append(suggests, prompt.Suggest{
					Text:        u.Name,
					SuggestType: prompt.SuggestOfHandler,
				})
			}
		}
	}
	return suggests, nil
}

func getNodeSuggest(input string) ([]prompt.Suggest, error) {
	nodeMutex.Lock()
	defer nodeMutex.Unlock()
	results := []prompt.Suggest{
		{
			Text:        "all",
			SuggestType: prompt.SuggestOfHandler,
		},
	}
	for _, node := range localNodeList {
		if prompt.IsMatch(input, node.GetName()) {
			results = append(results, prompt.Suggest{
				Text:        node.GetName(),
				SuggestType: prompt.SuggestOfHandler,
			})
		}
	}
	return results, nil
}
