package util

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

// 使用PKCS7标准进行填充
func PKCS7Padding(plaintext []byte, blockSize int) []byte {
	paddingNum := blockSize - len(plaintext)%blockSize
	plaintextWithPadding := bytes.Repeat([]byte{byte(paddingNum)}, paddingNum)
	return append(plaintext, plaintextWithPadding...)
}

// 使用PKCS7标准去除填充的数据
func PKCS7UnPadding(plaintextWithPadding []byte) ([]byte, error) {
	length := len(plaintextWithPadding)
	if length == 0 {
		return nil, fmt.Errorf("empty data")
	}
	unpaddingNum := int(plaintextWithPadding[length-1])
	if length-unpaddingNum < 0 {
		return nil, fmt.Errorf("invalid data")
	}
	return plaintextWithPadding[:(length - unpaddingNum)], nil
}

// 使用AES算法加密明文数据
func EncryptWithAES(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	blockSize := block.BlockSize()
	plaintextWithPadding := PKCS7Padding(plaintext, blockSize)
	blockMode := cipher.NewCBCEncrypter(block, key[:blockSize]) //初始向量的长度必须等于块block的长度16字节
	encryptedData := make([]byte, len(plaintextWithPadding))
	blockMode.CryptBlocks(encryptedData, plaintextWithPadding)
	return encryptedData, nil
}

// 使用AES算法解密加密数据
func DecryptWithAES(encryptedData, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	blockSize := block.BlockSize()
	blockMode := cipher.NewCBCDecrypter(block, key[:blockSize]) //初始向量的长度必须等于块block的长度16字节
	plaintextWithPadding := make([]byte, len(encryptedData))
	blockMode.CryptBlocks(plaintextWithPadding, encryptedData)
	return PKCS7UnPadding(plaintextWithPadding)
}
