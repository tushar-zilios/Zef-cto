package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL        string
	GeminiAPIKey       string
	JWTSecret          string
	JWTExpirationHours int
	Port               string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	expHours := 24
	if v := os.Getenv("JWT_EXPIRATION_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			expHours = n
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	return &Config{
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		GeminiAPIKey:       os.Getenv("GEMINI_API_KEY"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		JWTExpirationHours: expHours,
		Port:               port,
	}, nil
}
