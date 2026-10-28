package api

import (
	"crypto/aes"
	"encoding/hex"
	"errors"

	"github.com/88250/gulu"
	"github.com/siyuan-note/siyuan/kernel/util"
)

var errInvalidSyncProviderPackage = errors.New("invalid sync provider package")

// decodeSyncProviderConfig 读取既有配置包，损坏的密文或填充必须返回错误并保留当前配置。
func decodeSyncProviderConfig(data []byte, target any) (err error) {
	defer func() {
		if recover() != nil {
			err = errInvalidSyncProviderPackage
		}
	}()
	encrypted, err := hex.DecodeString(string(data))
	if err != nil || len(encrypted) == 0 || len(encrypted)%aes.BlockSize != 0 {
		return errInvalidSyncProviderPackage
	}
	plain, err := hex.DecodeString(string(util.AESDecrypt(string(data))))
	if err != nil || gulu.JSON.UnmarshalJSON(plain, target) != nil {
		return errInvalidSyncProviderPackage
	}
	return nil
}
