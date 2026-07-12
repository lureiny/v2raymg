package rpc

import (
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
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

// aadVersion domain-separates ciphertexts (protocol version). It is combined
// with the proto message type name so a ciphertext for one message type cannot
// be replayed as another (GCM authenticates the AAD).
const aadVersion = "v2raymg-rpc/v1"

func buildAAD(m proto.Message) []byte {
	name := string(proto.MessageName(m))
	aad := make([]byte, 0, len(aadVersion)+1+len(name))
	aad = append(aad, aadVersion...)
	aad = append(aad, '|')
	aad = append(aad, name...)
	return aad
}

func (e *EncryptMessageCodec) Marshal(v interface{}) ([]byte, error) {
	m, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("failed to marshal, message is %T, want proto.Message", v)
	}
	byteReq, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}
	return EncryptWithAES(byteReq, e.key, buildAAD(m))
}

func (e *EncryptMessageCodec) Unmarshal(data []byte, v interface{}) error {
	vv, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}
	// The destination message vv has the same proto type name the sender
	// Marshaled, so the AAD (version|type) matches; a mismatched type fails.
	decryptMessage, err := DecryptWithAES(data, e.key, buildAAD(vv))
	if err != nil {
		return err
	}
	return proto.Unmarshal(decryptMessage, vv)
}

func (e *EncryptMessageCodec) Name() string {
	return "EncryptMessageCodec"
}

const rpcServerKeyLen = 32

// rpcKDFSalt / rpcKDFInfo are fixed, non-secret HKDF context values.
var (
	rpcKDFSalt = []byte("v2raymg-rpc-kdf-salt-v1")
	rpcKDFInfo = []byte("v2raymg/cluster-rpc/aes-256-gcm/v1")
)

// GetRpcKeyByToken derives a 32-byte AES-256 key from the cluster token using
// HKDF-SHA256. Both sides call this identical function, so keys always match.
// Unlike the previous truncate/pad scheme, every byte of the token contributes
// and short/empty tokens no longer degrade to a low-entropy or constant key —
// though HKDF cannot add entropy, so a strong token is still required (enforced
// by appconfig.Validate's minimum-length check).
func GetRpcKeyByToken(token string) []byte {
	key := make([]byte, rpcServerKeyLen)
	r := hkdf.New(sha256.New, []byte(token), rpcKDFSalt, rpcKDFInfo)
	// 32 bytes is far below HKDF-SHA256's 255*32 limit, so ReadFull never fails.
	_, _ = io.ReadFull(r, key)
	return key
}
