package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"cto/src/internal/config"
	"cto/src/internal/db"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	pool, err := db.InitCTODB(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.CloseCTODB()

	// PostgreSQL block to truncate all user tables in the public schema
	truncateQuery := `
		DO $$ 
		DECLARE 
			r RECORD; 
		BEGIN 
			FOR r IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public') 
			LOOP 
				EXECUTE 'TRUNCATE TABLE ' || quote_ident(r.tablename) || ' RESTART IDENTITY CASCADE'; 
			END LOOP; 
		END $$;
	`

	fmt.Println("Truncating all tables in CTO database...")
	_, err = pool.Exec(ctx, truncateQuery)
	if err != nil {
		log.Fatalf("Failed to truncate tables: %v", err)
	}

	fmt.Println("CTO Database truncated successfully.")
}
