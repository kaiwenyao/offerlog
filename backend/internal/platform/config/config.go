package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Database struct {
	URL string
}

type HTTP struct {
	Addr         string
	PublicBase   string // e.g. https://offerlog.example.com
	SessionHours int
}

type ObjectStore struct {
	Provider     string // "local" or "s3"
	LocalDir     string
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	MaxFileBytes int64
	QuotaBytes   int64
	GracePeriod  time.Duration
}

type App struct {
	TimeZone   string
	LogLevel   string
	Env        string
	CSRFSecret string
	DataDir    string

	// RegistrationOpen gates the public /auth/register endpoint. Personal
	// instances stay closed by default; accounts come from api-admin
	// create-user or by flipping REGISTRATION_OPEN.
	RegistrationOpen bool
}

type Config struct {
	Database    Database
	HTTP        HTTP
	ObjectStore ObjectStore
	App         App
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getenvInt64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func getenvBool(key string, def bool) bool {
	v := strings.ToLower(os.Getenv(key))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func Load() (Config, error) {
	cfg := Config{
		Database: Database{URL: getenv("DATABASE_URL", "postgres://offerlog:offerlog@localhost:5432/offerlog?sslmode=disable")},
		HTTP: HTTP{
			Addr:         getenv("HTTP_ADDR", ":8080"),
			PublicBase:   strings.TrimRight(getenv("PUBLIC_BASE", "http://localhost:8080"), "/"),
			SessionHours: getenvInt("SESSION_HOURS", 24*14),
		},
		ObjectStore: ObjectStore{
			Provider:     getenv("OBJECTSTORE_PROVIDER", "local"),
			LocalDir:     getenv("OBJECTSTORE_LOCAL_DIR", "./data/objects"),
			Endpoint:     getenv("S3_ENDPOINT", "http://localhost:8333"),
			Region:       getenv("S3_REGION", "us-east-1"),
			Bucket:       getenv("S3_BUCKET", "offerlog"),
			AccessKey:    getenv("S3_ACCESS_KEY", ""),
			SecretKey:    getenv("S3_SECRET_KEY", ""),
			UsePathStyle: true,
			MaxFileBytes: getenvInt64("MAX_FILE_BYTES", 20*1024*1024),
			QuotaBytes:   getenvInt64("FILE_QUOTA_BYTES", 2*1024*1024*1024),
			GracePeriod:  time.Duration(getenvInt64("CLEANUP_GRACE_HOURS", 24)) * time.Hour,
		},
		App: App{
			TimeZone:   getenv("APP_TIMEZONE", "Europe/Dublin"),
			LogLevel:   getenv("LOG_LEVEL", "info"),
			Env:        getenv("APP_ENV", "development"),
			CSRFSecret: getenv("CSRF_SECRET", ""),
			DataDir:    getenv("DATA_DIR", "./data"),

			RegistrationOpen: getenvBool("REGISTRATION_OPEN", false),
		},
	}
	if cfg.ObjectStore.Provider == "s3" && (cfg.ObjectStore.AccessKey == "" || cfg.ObjectStore.SecretKey == "" || cfg.ObjectStore.Endpoint == "") {
		return cfg, fmt.Errorf("OBJECTSTORE_PROVIDER=s3 requires S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY")
	}
	return cfg, nil
}
