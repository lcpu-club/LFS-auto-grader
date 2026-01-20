package config

type ManagerConfig struct {
	Listen *string

	Endpoint  *string
	RunnerID  *string
	RunnerKey *string
	RateLimit *int64

	RedisConfig      *string
	SharedVolumePath *string

	TLSCertFile *string
	TLSKeyFile  *string
}
