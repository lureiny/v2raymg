//go:build v2ray

package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	protocolP "github.com/lureiny/v2raymg/proxy/protocol"
	"github.com/v2fly/v2ray-core/v5/app/proxyman/command"
	"github.com/v2fly/v2ray-core/v5/common/protocol"
	"github.com/v2fly/v2ray-core/v5/common/serial"
	conf "github.com/v2fly/v2ray-core/v5/infra/conf/v4"
	"github.com/v2fly/v2ray-core/v5/proxy/trojan"
	"github.com/v2fly/v2ray-core/v5/proxy/vless"
	"github.com/v2fly/v2ray-core/v5/proxy/vmess"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/runtime/protoiface"
)

type User struct {
	Tag      string
	Level    uint32
	Email    string
	AlterId  uint32
	UUID     string
	Account  protoiface.MessageV1
	Protocol string
}

const (
	VlessProtocolName  = "vless"
	TrojanProtocolName = "trojan"
	VmessProtocolName  = "vmess"
)

type UserOption func(*User)

func NewUser(email string, bound_tag string, options ...UserOption) (*User, error) {
	if email == "" {
		return nil, fmt.Errorf("User email can not be empty")
	}
	user := User{
		Tag:      bound_tag,
		Level:    0,
		Email:    email,
		AlterId:  0,
		UUID:     uuid.New().String(),
		Protocol: VmessProtocolName,
	}

	for _, option := range options {
		option(&user)
	}

	return &user, nil
}

func UUID(custon_uuid string) UserOption {
	return func(user *User) {
		if _, ok := uuid.Parse(custon_uuid); ok != nil {
			user.UUID = uuid.New().String()
		} else {
			user.UUID = custon_uuid
		}
	}
}

func Level(level uint32) UserOption {
	return func(user *User) {
		user.Level = level
	}
}

func Protocol(protocol string) UserOption {
	return func(user *User) {
		user.Protocol = protocol
	}
}

func SetUserAccount(user *User) error {
	switch strings.ToLower(user.Protocol) {
	case VmessProtocolName:
		user.Account = &vmess.Account{
			Id:               user.UUID,
			AlterId:          user.AlterId,
			SecuritySettings: &protocol.SecurityConfig{Type: protocol.SecurityType_AUTO},
		}
	case VlessProtocolName:
		user.Account = &vless.Account{
			Id: user.UUID,
		}
	case TrojanProtocolName:
		user.Account = &trojan.Account{
			Password: user.UUID,
		}
	default:
		fmt.Errorf(fmt.Sprintf("Unsupport protocol %s", user.Protocol))
	}
	return nil
}

func addUser(con command.HandlerServiceClient, user *User) error {
	_, err := con.AlterInbound(context.Background(), &command.AlterInboundRequest{
		Tag: user.Tag,
		Operation: serial.ToTypedMessage(&command.AddUserOperation{
			User: &protocol.User{
				Level:   user.Level,
				Email:   user.Email,
				Account: serial.ToTypedMessage(user.Account),
			},
		}),
	})
	return err
}

func addVmessUser(in *protocolP.InboundDetourConfig, user *User) error {
	vmessConfig, err := NewVmessInboundConfig(in)
	if err != nil {
		return err
	}

	c := protocolP.V2rayInboundUser{Email: user.Email, ID: user.UUID}
	cb, err := json.Marshal(c)
	if err != nil {
		return err
	}

	vmessConfig.Users = append(vmessConfig.Users, cb)
	vmessConfigBytes, err := json.MarshalIndent(vmessConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&vmessConfigBytes)
	return nil
}

func addVlessUser(in *protocolP.InboundDetourConfig, user *User) error {
	vlessConfig, err := NewVlessInboundConfig(in)
	if err != nil {
		return err
	}

	c := protocolP.V2rayInboundUser{Email: user.Email, ID: user.UUID}
	cb, err := json.Marshal(c)
	if err != nil {
		return err
	}

	vlessConfig.Clients = append(vlessConfig.Clients, cb)
	vlessConfigBytes, err := json.MarshalIndent(vlessConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&vlessConfigBytes)
	return nil
}

func addTrojanUser(in *protocolP.InboundDetourConfig, user *User) error {
	trojanConfig, err := NewTrojanInboundConfig(in)
	if err != nil {
		return err
	}

	trojanUser := conf.TrojanUserConfig{
		Password: user.UUID,
		Email:    user.Email,
		Level:    byte(user.Level),
	}

	trojanConfig.Clients = append(trojanConfig.Clients, &trojanUser)
	trojanConfigBytes, err := json.MarshalIndent(trojanConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&trojanConfigBytes)
	return nil
}

func addUserToRuntime(runtimeConfig *RuntimeConfig, user *User) error {
	// 创建grpc client
	cmdConn, err := grpc.Dial(fmt.Sprintf("%s:%d", runtimeConfig.Host, runtimeConfig.Port), grpc.WithInsecure())
	if err != nil {
		return err
	}

	handlerClient := command.NewHandlerServiceClient(cmdConn)

	err = addUser(handlerClient, user)
	if err != nil {
		return err
	}

	log.Printf("Add user to runtime: [Email] %s, [UUID/Password] %s to [Bound] %s", user.Email, user.UUID, user.Tag)
	return nil
}

func removeUser(con command.HandlerServiceClient, user *User) error {
	_, err := con.AlterInbound(context.Background(), &command.AlterInboundRequest{
		Tag: user.Tag,
		Operation: serial.ToTypedMessage(&command.RemoveUserOperation{
			Email: user.Email,
		}),
	})
	return err
}

func removeVmessUser(in *protocolP.InboundDetourConfig, user *User) error {
	vmessConfig, err := NewVmessInboundConfig(in)
	if err != nil {
		return err
	}

	for index := range vmessConfig.Users {
		var vmessUser protocolP.V2rayInboundUser
		json.Unmarshal(vmessConfig.Users[index], &vmessUser)
		if vmessUser.Email == user.Email {
			vmessConfig.Users = append(vmessConfig.Users[:index], vmessConfig.Users[index+1:]...)
			break
		}
		if index == len(vmessConfig.Users)-1 {
			return fmt.Errorf("No User " + user.Email)
		}
	}

	vmessConfigBytes, err := json.MarshalIndent(vmessConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&vmessConfigBytes)
	return nil
}

func removeVlessUser(in *protocolP.InboundDetourConfig, user *User) error {
	vlessConfig, err := NewVlessInboundConfig(in)
	if err != nil {
		return err
	}

	for index := range vlessConfig.Clients {
		var vlessUser protocolP.V2rayInboundUser
		json.Unmarshal(vlessConfig.Clients[index], &vlessUser)
		if vlessUser.Email == user.Email {
			vlessConfig.Clients = append(vlessConfig.Clients[:index], vlessConfig.Clients[index+1:]...)
			break
		}
		if index == len(vlessConfig.Clients)-1 {
			return fmt.Errorf(" " + user.Email)
		}
	}

	vlessConfigBytes, err := json.MarshalIndent(vlessConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&vlessConfigBytes)
	return nil
}

func removeTrojanUser(in *protocolP.InboundDetourConfig, user *User) error {
	trojanConfig, err := NewTrojanInboundConfig(in)
	if err != nil {
		return err
	}

	for index := range trojanConfig.Clients {
		c := trojanConfig.Clients[index]
		if c.Email == user.Email {
			trojanConfig.Clients = append(trojanConfig.Clients[:index], trojanConfig.Clients[index+1:]...)
			break
		}
		if index == len(trojanConfig.Clients)-1 {
			return fmt.Errorf(" " + user.Email)
		}
	}

	trojanConfigBytes, err := json.MarshalIndent(trojanConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&trojanConfigBytes)
	return nil
}

func removeUserFromRuntime(runtimeConfig *RuntimeConfig, user *User) error {
	// 创建grpc client
	cmdConn, err := grpc.Dial(fmt.Sprintf("%s:%d", runtimeConfig.Host, runtimeConfig.Port), grpc.WithInsecure())
	if err != nil {
		return err
	}

	handlerClient := command.NewHandlerServiceClient(cmdConn)

	if err != nil {
		return err
	}

	err = removeUser(handlerClient, user)
	if err != nil {
		return err
	}
	log.Printf("Remove User from runtime: [Email] %s from [Bound] %s", user.Email, user.Tag)

	return nil
}

func CompleteUserInformation(user *User, inbound *Inbound) error {
	if inbound == nil {
		return fmt.Errorf("inbound with tag(%s) is not exist", user.Tag)
	}
	inbound.RWMutex.RLock()
	defer inbound.RWMutex.RUnlock()
	user.Protocol = inbound.Config.Protocol
	// 设置protocol后需要重新设置account
	return SetUserAccount(user)
}

func GetInboundUsers(in *protocolP.InboundDetourConfig) []string {
	switch strings.ToLower(in.Protocol) {
	case VlessProtocolName:
		return getVlessUsers(in)
	case VmessProtocolName:
		return getVmessUsers(in)
	case TrojanProtocolName:
		return getTrojanUsers(in)
	default:
		return []string{}
	}
}

func getVmessUsers(in *protocolP.InboundDetourConfig) []string {
	users := []string{}
	vmessConfig, err := NewVmessInboundConfig(in)
	if err != nil {
		return users
	}

	for _, user := range vmessConfig.Users {
		vUser := protocolP.V2rayInboundUser{}
		err := json.Unmarshal(user, &vUser)
		if err != nil {
			continue
		}
		users = append(users, vUser.Email)
	}
	return users
}

func getVlessUsers(in *protocolP.InboundDetourConfig) []string {
	users := []string{}
	vlessConfig, err := NewVlessInboundConfig(in)
	if err != nil {
		return users
	}

	for _, user := range vlessConfig.Clients {
		vUser := protocolP.V2rayInboundUser{}
		err := json.Unmarshal(user, &vUser)
		if err != nil {
			continue
		}
		users = append(users, vUser.Email)
	}
	return users
}

func getTrojanUsers(in *protocolP.InboundDetourConfig) []string {
	users := []string{}
	trojanConfig, err := NewTrojanInboundConfig(in)
	if err != nil {
		return users
	}

	for _, user := range trojanConfig.Clients {
		users = append(users, user.Email)
	}
	return users
}

func NewVlessInboundConfig(in *protocolP.InboundDetourConfig) (*protocolP.VLessInboundConfig, error) {
	if strings.ToLower(in.Protocol) != VlessProtocolName {
		return nil, fmt.Errorf("wrong protocol, need %s, but %s", VlessProtocolName, in.Protocol)
	}
	vlessInboundConfig := new(protocolP.VLessInboundConfig)
	err := json.Unmarshal([]byte(*(in.Settings)), vlessInboundConfig)
	if err != nil {
		return nil, err
	}
	return vlessInboundConfig, nil
}

func NewVmessInboundConfig(in *protocolP.InboundDetourConfig) (*conf.VMessInboundConfig, error) {
	if strings.ToLower(in.Protocol) != VmessProtocolName {
		return nil, fmt.Errorf("wrong protocol, need %s, but %s", VmessProtocolName, in.Protocol)
	}
	vmessInboundConfig := new(conf.VMessInboundConfig)
	err := json.Unmarshal([]byte(*(in.Settings)), vmessInboundConfig)
	if err != nil {
		return nil, err
	}
	return vmessInboundConfig, nil
}

func NewTrojanInboundConfig(in *protocolP.InboundDetourConfig) (*conf.TrojanServerConfig, error) {
	if strings.ToLower(in.Protocol) != TrojanProtocolName {
		return nil, fmt.Errorf("wrong protocol, need %s, but %s", TrojanProtocolName, in.Protocol)
	}
	trojanInboundConfig := new(conf.TrojanServerConfig)
	err := json.Unmarshal([]byte(*(in.Settings)), trojanInboundConfig)
	if err != nil {
		return nil, err
	}
	return trojanInboundConfig, nil
}
