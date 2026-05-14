package config

import (
	"os"
	"strconv"
)

type Config struct {
	IndexPath     string
	IvfNprobe     int
	IvfFullNprobe int
	Candidates    int
	UDSPath       string
	UDSMode       int
	UnlinkUDS     bool
}

func Load() *Config {
	return &Config{
		IndexPath:     envStr("INDEX_PATH", "resources/index.bin"),
		IvfNprobe:     envInt("IVF_NPROBE", 8, 1, 64),
		IvfFullNprobe: envInt("IVF_FULL_NPROBE", 24, 1, 64),
		Candidates:    envInt("CANDIDATES", 0, 0, 2000000),
		UDSPath:       envStr("UDS_PATH", envStr("SOCKET_PATH", "/tmp/rinha.sock")),
		UDSMode:       envInt("UDS_MODE", 666, 0, 777),
		UnlinkUDS:     envInt("UNLINK_UDS", 1, 0, 1) == 1,
	}
}

func envStr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func envInt(name string, def, minv, maxv int) int {
	v, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return def
	}
	if v < minv {
		return minv
	}
	if v > maxv {
		return maxv
	}
	return v
}
