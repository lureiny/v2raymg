package bound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/config"
	"github.com/lureiny/v2raymg/fileIO"
	protocolP "github.com/lureiny/v2raymg/protocol"
	"github.com/v2fly/v2ray-core/v4/app/proxyman/command"
	"github.com/v2fly/v2ray-core/v4/common/protocol"
	"github.com/v2fly/v2ray-core/v4/common/serial"
	"github.com/v2fly/v2ray-core/v4/infra/conf"
	"github.com/v2fly/v2ray-core/v4/proxy/vless"
	"github.com/v2fly/v2ray-core/v4/proxy/vmess"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/runtime/protoiface"
)

type User struct {
	InBoundTag string
	Level      uint32
	Email      string
	AlterId    uint32
	UUID       string
	Account    protoiface.MessageV1
	Protocol   string
}

type UserOption func(*User)

func NewUser(email string, bound_tag string, options ...UserOption) (*User, error) {
	if email == "" {
		return nil, errors.New("User email can not be empty")
	}
	user := User{
		InBoundTag: bound_tag,
		Level:      0,
		Email:      email,
		AlterId:    0,
		UUID:       uuid.New().String(),
		Protocol:   "vmess",
	}

	for _, option := range options {
		option(&user)
	}

	// 生成对应Account
	setUserAccount(&user)
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

func setUserAccount(user *User) {
	switch strings.ToLower(user.Protocol) {
	case "vmess":
		user.Account = &vmess.Account{
			Id:               user.UUID,
			AlterId:          user.AlterId,
			SecuritySettings: &protocol.SecurityConfig{Type: protocol.SecurityType_AUTO},
		}
	case "vless":
		user.Account = &vless.Account{
			Id: user.UUID,
		}
	default:
		config.Error.Printf("Unsupport protocol %s", user.Protocol)
	}
}

// GetProtocol 根据tag查寻对应inbound的protocol
func GetProtocol(tag string, file string) (string, error) {
	config, err := fileIO.LoadConfig(file)
	if err != nil {
		return "", err
	}
	for _, in := range config.InboundConfigs {
		if in.Tag == tag {
			return in.Protocol, nil
		}
	}
	return "", errors.New(fmt.Sprintf("Not found inbound with %v", tag))
}

func addUser(con command.HandlerServiceClient, user *User) error {
	_, err := con.AlterInbound(context.Background(), &command.AlterInboundRequest{
		Tag: user.InBoundTag,
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

func removeUser(con command.HandlerServiceClient, user *User) error {
	_, err := con.AlterInbound(context.Background(), &command.AlterInboundRequest{
		Tag: user.InBoundTag,
		Operation: serial.ToTypedMessage(&command.RemoveUserOperation{
			Email: user.Email,
		}),
	})
	return err
}

func addUserToRuntime(runtimeConfig *config.RuntimeConfig, user *User) error {
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

	config.Info.Printf("Add user to runtime: [Email] %s, [UUID] %s to [Bound] %s", user.Email, user.UUID, user.InBoundTag)
	return nil
}

// AddUser 添加用户, 同时添加的运行中的程序以及配置文件中
func AddUser(runtimeConfig *config.RuntimeConfig, user *User) error {
	err := addUserToRuntime(runtimeConfig, user)
	if err != nil {
		return err
	}

	if err := addUserToFile(user, runtimeConfig.ConfigFile); err != nil {
		return err
	}

	return nil
}

func addUserToFile(user *User, configFile string) error {
	c, err := fileIO.LoadConfig(configFile)
	if err != nil {
		return err
	}

	if err := addUserToConfig(c, user); err != nil {
		return err
	}

	if err := fileIO.DumpConfig(c, configFile); err != nil {
		return err
	}

	config.Info.Printf("Add user to config file: [Email] %s, [UUID] %s to [Bound] %s", user.Email, user.UUID, user.InBoundTag)
	return nil
}

func addUserToConfig(c *protocolP.V2rayConfig, user *User) error {
	for index := range c.InboundConfigs {
		inBound := &(c.InboundConfigs[index])
		if inBound.Tag == user.InBoundTag {
			switch strings.ToLower(inBound.Protocol) {
			// 添加用户前应先检测是否已经存在
			case "vmess":
				return addVmessUser(inBound, user)
			case "vless":
				return addVlessUser(inBound, user)
			}
		}
	}

	return errors.New("No inbound which has tag: " + user.InBoundTag)
}

func addVmessUser(in *protocolP.InboundDetourConfig, user *User) error {
	vmessConfig := new(conf.VMessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vmessConfig)
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
	vlessConfig := new(protocolP.VLessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vlessConfig)
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

func removeVmessUser(in *protocolP.InboundDetourConfig, user *User) error {
	vmessConfig := new(conf.VMessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vmessConfig)
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
			return errors.New("No User " + user.Email)
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
	vlessConfig := new(protocolP.VLessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vlessConfig)
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
			return errors.New(" " + user.Email)
		}
	}

	vlessConfigBytes, err := json.MarshalIndent(vlessConfig, "", "    ")
	if err != nil {
		return err
	}

	in.Settings = (*json.RawMessage)(&vlessConfigBytes)
	return nil
}

func RemoveUser(runtimeConfig *config.RuntimeConfig, user *User) error {
	if err := removeUserFromRuntime(runtimeConfig, user); err != nil {
		return err
	}

	if err := removeUserFromFile(user, runtimeConfig.ConfigFile); err != nil {
		return err
	}

	return nil
}

func removeUserFromFile(user *User, configFile string) error {
	c, err := fileIO.LoadConfig(configFile)
	if err != nil {
		return err
	}

	if err := removeUserFromConfig(c, user); err != nil {
		return err
	}

	if err := fileIO.DumpConfig(c, configFile); err != nil {
		return err
	}

	config.Info.Printf("Remove user from config file: [Email] %s from [Bound] %s", user.Email, user.InBoundTag)
	return nil
}

func removeUserFromConfig(c *protocolP.V2rayConfig, user *User) error {
	for index := range c.InboundConfigs {
		inBound := &(c.InboundConfigs[index])
		if inBound.Tag == user.InBoundTag {
			switch strings.ToLower(inBound.Protocol) {
			// 添加用户前应先检测是否已经存在
			case "vmess":
				return removeVmessUser(inBound, user)
			case "vless":
				return removeVlessUser(inBound, user)
			}
		}
	}

	return errors.New("No inbound which has tag: " + user.InBoundTag)
}

func removeUserFromRuntime(runtimeConfig *config.RuntimeConfig, user *User) error {
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
	config.Info.Printf("Remove User from runtime: [Email] %s from [Bound] %s", user.Email, user.InBoundTag)

	return nil
}
