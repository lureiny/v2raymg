package bound

import (
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/config"
	"github.com/v2fly/v2ray-core/v4/common/protocol"
	"github.com/v2fly/v2ray-core/v4/proxy/vless"
	"github.com/v2fly/v2ray-core/v4/proxy/vmess"
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
		config.Error.Fatalf("Unsupport protocol %s", user.Protocol)
	}
}
