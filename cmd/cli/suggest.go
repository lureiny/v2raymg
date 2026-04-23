package cli

import (
	"strings"

	"github.com/lureiny/go-prompt"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
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

	enablePingCheckSuggest = prompt.Suggest{
		Text:        "enable_ping_check",
		Description: "enable ping check",
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
		Description: "is xtls (deprecated: use security field instead)",
		Default:     false,
	}

	securitySuggest = prompt.Suggest{
		Text:        "security",
		Description: "security type: tls(default), xtls, reality",
		Default:     "tls",
	}

	realityTargetSuggest = prompt.Suggest{
		Text:        "reality_target",
		Description: "reality target server, e.g. www.example.com:443 (required when security=reality)",
		Default:     "",
	}

	realityServerNamesSuggest = prompt.Suggest{
		Text:        "reality_server_names",
		Description: "allowed SNI list for reality, comma-separated, e.g. www.example.com,example.com",
		Default:     "",
	}

	realityShortIDsSuggest = prompt.Suggest{
		Text:        "reality_short_ids",
		Description: "short ID list for reality, comma-separated hex strings",
		Default:     "",
	}

	containerSuggest = prompt.Suggest{
		Text:        "container",
		Description: "container type, xray( default) or snell",
		Default:     "xray",
	}

	portSuggest = prompt.Suggest{
		Text:        "port",
		Description: "inbound port, if port == 0, will generate a random port in 10000-50000",
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

	oldPasswordSuggest = prompt.Suggest{
		Text:        "old_password",
		Description: "current password",
		Default:     "",
	}

	newPasswordSuggest = prompt.Suggest{
		Text:        "new_password",
		Description: "new password",
		Default:     "",
	}

	expireSuggest = prompt.Suggest{
		Text:        "expire",
		Description: "user expire time, timestamp, 0 no expire",
		Default:     int(0),
	}

	ttlSuggest = prompt.Suggest{
		Text:        "ttl",
		Description: "use to clac user expire time, user expire time = ttl + current time",
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

	boundRawStringSuggest = prompt.Suggest{
		Text:        "bound_raw_string",
		Description: "base64-encoded inbound config json",
		Default:     "",
	}

	versionTagSuggest = prompt.Suggest{
		Text:        "version_tag",
		Description: "proxy version tag, default latest",
		Default:     "latest",
	}

	inboundNameSuggest = prompt.Suggest{
		Text:        "name",
		Description: "inbound name (tag within the container)",
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
		inputs = append(inputs, "")
	}
	notInputHandler := len(inputs) > 1
	isInputParamValue := notInputHandler &&
		(prompt.IsBoolSuggest(h.Suggests, inputs[len(inputs)-1], h.SuggestPrefix) ||
			prompt.IsInputNotBoolValue(inputs, h.SuggestPrefix, h.Suggests))
	if isInputParamValue {
		lastFlag := inputs[len(inputs)-2]
		currentInput := inputs[len(inputs)-1]

		if isInputNodeName(lastFlag, h.SuggestPrefix) {
			return getNodeSuggest(currentInput)
		}

		if isInputUserName(lastFlag, h.SuggestPrefix, inputs[0]) {
			return getUserSuggest(getTargetParam(input), currentInput)
		}

		if isInputContainerName(lastFlag, h.SuggestPrefix) {
			return getContainerSuggest(currentInput)
		}

		if isInputInboundName(lastFlag, h.SuggestPrefix) {
			return getInboundNameSuggest(getTargetParam(input), getContainerParam(input), currentInput)
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

func isInputContainerName(lastFlag, suggestPrefix string) bool {
	return lastFlag == suggestPrefix+containerSuggest.Text
}

func isInputInboundName(lastFlag, suggestPrefix string) bool {
	return lastFlag == suggestPrefix+tagSuggest.Text ||
		lastFlag == suggestPrefix+srcTagSuggest.Text ||
		lastFlag == suggestPrefix+dstTagSuggest.Text ||
		lastFlag == suggestPrefix+inboundNameSuggest.Text
}

// getTargetParam extracts the value of -target from the current input line.
func getTargetParam(input string) string {
	return getParamValue(input, targetSuggest.Text)
}

// getContainerParam extracts the value of -container from the current input line.
func getContainerParam(input string) string {
	return getParamValue(input, containerSuggest.Text)
}

// getParamValue extracts the value of a named flag (e.g. "-target") from the input line.
func getParamValue(input, paramName string) string {
	splitedInput := strings.Split(input, " ")
	inputs := []string{}
	for _, s := range splitedInput {
		if len(s) > 0 {
			inputs = append(inputs, s)
		}
	}
	flag := "-" + paramName
	for index, i := range inputs {
		if i == flag && index != len(inputs)-1 {
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

// knownContainers lists all supported container types for inbound autocomplete.
// Keep in sync with contracts.ContainerType constants in
// pkg/proxy/core/contracts/protocol.go.
var knownContainers = []string{"xray", "snell", "hysteria", "mihomo"}

// getContainerSuggest returns fixed container type suggestions.
func getContainerSuggest(currentInput string) ([]prompt.Suggest, error) {
	results := []prompt.Suggest{}
	for _, ct := range knownContainers {
		if prompt.IsMatch(currentInput, ct) {
			results = append(results, prompt.Suggest{
				Text:        ct,
				SuggestType: prompt.SuggestOfHandler,
			})
		}
	}
	return results, nil
}

// getInboundNameSuggest returns inbound name suggestions.
// If target is set, only inbounds from that node are considered.
// If container is set, only inbounds of that container type are considered.
// Results are deduplicated by name.
func getInboundNameSuggest(target, container, currentInput string) ([]prompt.Suggest, error) {
	inboundMutex.Lock()
	defer inboundMutex.Unlock()

	seen := map[string]bool{}
	results := []prompt.Suggest{}

	addInbound := func(inb *proto.InboundInfo) {
		if container != "" && inb.GetContainer() != container {
			return
		}
		name := inb.GetName()
		if seen[name] {
			return
		}
		if prompt.IsMatch(currentInput, name) {
			seen[name] = true
			results = append(results, prompt.Suggest{
				Text:        name,
				Description: inb.GetContainer(),
				SuggestType: prompt.SuggestOfHandler,
			})
		}
	}

	if target != "" {
		// specific node
		for _, inb := range localInboundList[target] {
			addInbound(inb)
		}
	} else {
		// all nodes
		for _, inbounds := range localInboundList {
			for _, inb := range inbounds {
				addInbound(inb)
			}
		}
	}

	return results, nil
}
