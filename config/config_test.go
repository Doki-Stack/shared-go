package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_FromEnv(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer os.Unsetenv("PORT")
	defer os.Unsetenv("DATABASE_URL")

	var cfg struct {
		Port        int    `env:"PORT" default:"8080"`
		DatabaseURL string `env:"DATABASE_URL" required:"true"`
	}
	err := Load(&cfg)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://localhost/test" {
		t.Errorf("DatabaseURL = %q, want postgres://localhost/test", cfg.DatabaseURL)
	}
}

func TestLoad_Defaults(t *testing.T) {
	os.Unsetenv("PORT")
	var cfg struct {
		Port int `env:"PORT" default:"8080"`
	}
	err := Load(&cfg)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
}

func TestLoad_RequiredMissing(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	var cfg struct {
		DatabaseURL string `env:"DATABASE_URL" required:"true"`
	}
	err := Load(&cfg)
	if err == nil {
		t.Fatal("Load should fail when required field is missing")
	}
	if err != nil && err.Error() != "required config DATABASE_URL not set" {
		t.Errorf("err = %v, want required config DATABASE_URL not set", err)
	}
}

func TestLoad_Duration(t *testing.T) {
	os.Setenv("READ_TIMEOUT", "45s")
	defer os.Unsetenv("READ_TIMEOUT")
	var cfg struct {
		ReadTimeout time.Duration `env:"READ_TIMEOUT" default:"30s"`
	}
	err := Load(&cfg)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ReadTimeout != 45*time.Second {
		t.Errorf("ReadTimeout = %v, want 45s", cfg.ReadTimeout)
	}
}

func TestLoad_Bool(t *testing.T) {
	tests := []struct {
		env  string
		want bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
	}
	for _, tt := range tests {
		os.Setenv("DEBUG", tt.env)
		var cfg struct {
			Debug bool `env:"DEBUG" default:"false"`
		}
		err := Load(&cfg)
		os.Unsetenv("DEBUG")
		if err != nil {
			t.Fatalf("Load failed for %q: %v", tt.env, err)
		}
		if cfg.Debug != tt.want {
			t.Errorf("Debug = %v for env %q, want %v", cfg.Debug, tt.env, tt.want)
		}
	}
}

func TestLoad_Int64(t *testing.T) {
	os.Setenv("MAX_SIZE", "1234567890")
	defer os.Unsetenv("MAX_SIZE")
	var cfg struct {
		MaxSize int64 `env:"MAX_SIZE" default:"100"`
	}
	err := Load(&cfg)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.MaxSize != 1234567890 {
		t.Errorf("MaxSize = %d, want 1234567890", cfg.MaxSize)
	}
}

func TestLoad_Float64(t *testing.T) {
	os.Setenv("RATIO", "1.5")
	defer os.Unsetenv("RATIO")
	var cfg struct {
		Ratio float64 `env:"RATIO" default:"1.0"`
	}
	err := Load(&cfg)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Ratio != 1.5 {
		t.Errorf("Ratio = %f, want 1.5", cfg.Ratio)
	}
}

func TestLoad_StringSlice(t *testing.T) {
	os.Setenv("HOSTS", "a,b,c")
	defer os.Unsetenv("HOSTS")
	var cfg struct {
		Hosts []string `env:"HOSTS" default:"localhost"`
	}
	err := Load(&cfg)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Hosts) != 3 || cfg.Hosts[0] != "a" || cfg.Hosts[1] != "b" || cfg.Hosts[2] != "c" {
		t.Errorf("Hosts = %v, want [a b c]", cfg.Hosts)
	}
}

func TestLoad_Prefix(t *testing.T) {
	os.Setenv("API_PORT", "7070")
	defer os.Unsetenv("API_PORT")
	var cfg struct {
		Port int `env:"PORT" default:"8080"`
	}
	err := Load(&cfg, WithPrefix("API_"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Port != 7070 {
		t.Errorf("Port = %d, want 7070", cfg.Port)
	}
}

func TestLoad_WithDotenv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("DOTENV_PORT=6060\nDOTENV_HOST=localhost\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	var cfg struct {
		Port int    `env:"PORT" default:"8080"`
		Host string `env:"HOST" default:""`
	}
	err := Load(&cfg, WithPrefix("DOTENV_"), WithDotenv(envPath))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Port != 6060 {
		t.Errorf("Port = %d, want 6060", cfg.Port)
	}
	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want localhost", cfg.Host)
	}
}

func TestLoad_InvalidStruct(t *testing.T) {
	var x int
	err := Load(x)
	if err == nil {
		t.Fatal("Load should fail for non-pointer")
	}
	err = Load(&x)
	if err == nil {
		t.Fatal("Load should fail for non-struct")
	}
}

func TestLoad_InvalidType(t *testing.T) {
	os.Setenv("PORT", "not-a-number")
	defer os.Unsetenv("PORT")
	var cfg struct {
		Port int `env:"PORT" default:"8080"`
	}
	err := Load(&cfg)
	if err == nil {
		t.Fatal("Load should fail for invalid int")
	}
}

func TestLoad_InvalidDuration(t *testing.T) {
	os.Setenv("READ_TIMEOUT", "invalid")
	defer os.Unsetenv("READ_TIMEOUT")
	var cfg struct {
		ReadTimeout time.Duration `env:"READ_TIMEOUT" default:"30s"`
	}
	err := Load(&cfg)
	if err == nil {
		t.Fatal("Load should fail for invalid duration")
	}
}

func TestLoad_DotenvNotFound(t *testing.T) {
	var cfg struct {
		Port int `env:"PORT" default:"8080"`
	}
	err := Load(&cfg, WithDotenv("/nonexistent/path/.env"))
	if err == nil {
		t.Fatal("Load should fail when .env file not found")
	}
}
