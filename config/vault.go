package config

import (
	"fmt"
	"reflect"
	"strings"

	vault "github.com/hashicorp/vault/api"
)

// VaultConfig configures Vault secret loading.
type VaultConfig struct {
	Address string // VAULT_ADDR
	Token   string // VAULT_TOKEN (dev only; prod uses K8s auth)
	Path    string // secret/data/services/{service-name}
}

// loadFromVault reads KV v2 secrets from the specified path and merges into cfg.
// Uses github.com/hashicorp/vault/api.
// Vault keys map to struct fields by env tag name (case-insensitive).
// Vault secrets override existing values in cfg.
func loadFromVault(vc VaultConfig, cfg interface{}) error {
	config := vault.DefaultConfig()
	if vc.Address != "" {
		config.Address = vc.Address
	}
	client, err := vault.NewClient(config)
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	if vc.Token != "" {
		client.SetToken(vc.Token)
	}
	if vc.Path == "" {
		return fmt.Errorf("vault path not set")
	}

	secret, err := client.Logical().Read(vc.Path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if secret == nil {
		return nil
	}

	// KV v2 stores data under "data"
	dataMap, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		// Fallback: maybe it's KV v1 or raw
		dataMap = secret.Data
	}

	v := reflect.ValueOf(cfg)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanSet() {
			continue
		}
		sf := t.Field(i)
		envTag := sf.Tag.Get("env")
		if envTag == "" || envTag == "-" {
			continue
		}
		envKeyNorm := strings.ToLower(strings.ReplaceAll(envTag, "-", "_"))

		var raw interface{}
		for k, val := range dataMap {
			kNorm := strings.ToLower(strings.ReplaceAll(k, "-", "_"))
			if kNorm == envKeyNorm {
				raw = val
				break
			}
		}
		if raw == nil {
			continue
		}
		val, ok := raw.(string)
		if !ok {
			if s, ok := raw.(fmt.Stringer); ok {
				val = s.String()
			} else {
				val = fmt.Sprintf("%v", raw)
			}
		}
		if val == "" {
			continue
		}
		if err := setField(field, val, envTag); err != nil {
			return err
		}
	}
	return nil
}
