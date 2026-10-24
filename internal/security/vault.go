package security

import (
	"auto-check-in/internal/domain"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Vault struct{ aead cipher.AEAD }

func NewVault(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("主密钥必须是 32 字节")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}

func (v *Vault) Seal(plain []byte, purpose string) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{1}, nonce...)
	return v.aead.Seal(out, nonce, plain, []byte(purpose)), nil
}

func (v *Vault) Open(sealed []byte, purpose string) ([]byte, error) {
	n := v.aead.NonceSize()
	if len(sealed) < 1+n+v.aead.Overhead() || sealed[0] != 1 {
		return nil, errors.New("不支持或损坏的密文")
	}
	b, err := v.aead.Open(nil, sealed[1:1+n], sealed[1+n:], []byte(purpose))
	if err != nil {
		return nil, errors.New("解密失败：密钥不匹配或数据损坏")
	}
	return b, nil
}

func LoadKey(dir, configured string) ([]byte, error) {
	if configured != "" {
		return decodeKey(configured)
	}
	path := filepath.Join(dir, "master.key")
	b, err := os.ReadFile(path)
	if err == nil {
		return decodeKey(strings.TrimSpace(string(b)))
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, "checkin.db")); err == nil {
		return nil, errors.New("数据库存在但 master.key 丢失；请恢复原密钥或设置 CHECKIN_MASTER_KEY")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := writeExclusive(path, []byte(base64.StdEncoding.EncodeToString(key))); err != nil {
		return nil, err
	}
	return key, nil
}

func decodeKey(s string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(key) != 32 {
		return nil, errors.New("CHECKIN_MASTER_KEY / master.key 必须是 32 字节的 Base64 密钥")
	}
	return key, nil
}

func AccessToken(dir, configured string) (string, error) {
	if configured != "" {
		if len(configured) < 32 || len(configured) > 512 || strings.ContainsAny(configured, "\r\n ") {
			return "", errors.New("管理访问令牌须为 32 至 512 个无空白字符")
		}
		return configured, nil
	}
	path := filepath.Join(dir, "access.token")
	b, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(b))
		if len(token) < 32 || len(token) > 512 {
			return "", errors.New("access.token 已损坏")
		}
		return token, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, writeExclusive(path, []byte(token))
}

func writeExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

type backupEnvelope struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	Salt    string `json:"salt"`
	Payload string `json:"payload"`
}

func ValidateBackupPassword(password string) error {
	if len(password) < 12 || len(password) > 1024 {
		return errors.New("备份口令须为 12 至 1024 个字节")
	}
	return nil
}

func EncryptBackup(plain []byte, password string) ([]byte, error) {
	if err := ValidateBackupPassword(password); err != nil {
		return nil, err
	}
	if len(plain) > domain.MaxBackupBytes {
		return nil, errors.New("备份超出 32 MiB 限制，请先清理历史记录")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	v, err := backupVault(password, salt)
	if err != nil {
		return nil, err
	}
	sealed, err := v.Seal(plain, "auto-check-in-backup:v1")
	if err != nil {
		return nil, err
	}
	return json.Marshal(backupEnvelope{"auto-check-in-backup", 1, base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(sealed)})
}

func DecryptBackup(archive []byte, password string) ([]byte, error) {
	if len(archive) > domain.MaxBackupArchiveBytes || len(password) > 1024 {
		return nil, errors.New("备份或口令超出限制")
	}
	var env backupEnvelope
	if err := json.Unmarshal(archive, &env); err != nil || env.Format != "auto-check-in-backup" || env.Version != 1 {
		return nil, errors.New("不支持的备份格式或版本")
	}
	salt, err := base64.StdEncoding.DecodeString(env.Salt)
	if err != nil || len(salt) != 16 {
		return nil, errors.New("无效的备份盐")
	}
	payload, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil || len(payload) > domain.MaxBackupBytes+64 {
		return nil, errors.New("无效的备份内容")
	}
	v, err := backupVault(password, salt)
	if err != nil {
		return nil, err
	}
	plain, err := v.Open(payload, "auto-check-in-backup:v1")
	if err != nil {
		return nil, fmt.Errorf("备份口令错误或文件损坏")
	}
	return plain, nil
}

func backupVault(password string, salt []byte) (*Vault, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		return nil, err
	}
	return NewVault(key)
}
