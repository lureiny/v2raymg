package rpc

import (
	"fmt"

	"github.com/lureiny/v2raymg/common/util"
	"google.golang.org/protobuf/proto"
)

type EncryptMessageCodec struct {
	key []byte
}

func NewEncryptMessageCodec(token string) *EncryptMessageCodec {
	return &EncryptMessageCodec{
		key: GetRpcKeyByToken(token),
	}
}

func (e *EncryptMessageCodec) Marshal(v interface{}) ([]byte, error) {
	m := v.(proto.Message)
	byteReq, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}
	return util.EncryptWithAES(byteReq, e.key)
}

func (e *EncryptMessageCodec) Unmarshal(data []byte, v interface{}) error {
	decryptMessage, err := util.DecryptWithAES(data, e.key)
	if err != nil {
		return err
	}
	vv, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}
	return proto.Unmarshal(decryptMessage, vv)
}

func (e *EncryptMessageCodec) Name() string {
	return "EncryptMessageCodec"
}

const rpcServerKeyLen = 32

func GetRpcKeyByToken(token string) []byte {
	if len(token) >= rpcServerKeyLen {
		return []byte(token)[:32]
	} else {
		// 如果密码为空, 则同样不具有安全性, 仅仅不会被抓包直接分析
		return util.PKCS7Padding([]byte(token), rpcServerKeyLen)
	}
}
