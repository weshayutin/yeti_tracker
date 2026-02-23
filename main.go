package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

func main() {
	_ = godotenv.Load()

	dbHost := mustEnv("DB_HOST")
	dbPort := getEnv("DB_PORT", "3306")
	dbUser := mustEnv("DB_USER")
	dbPass := mustEnv("DB_PASSWORD")
	dbNorth := getEnv("DB_NAME_NORTH", "f3denver")
	dbSouth := getEnv("DB_NAME_SOUTH", "f3denversouth")
	defaultStartDate := getEnv("DEFAULT_START_DATE", "2025-12-21")
	defaultEndDate := getEnv("DEFAULT_END_DATE", "2026-03-20")
	serverPort := getEnv("SERVER_PORT", "8080")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=false&timeout=10s",
		dbUser, dbPass, dbHost, dbPort, dbNorth)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	log.Println("connected to MySQL")

	app := &App{
		DB:               db,
		DBNorth:          dbNorth,
		DBSouth:          dbSouth,
		DefaultStartDate: defaultStartDate,
		DefaultEndDate:   defaultEndDate,
	}

	http.HandleFunc("/", app.IndexHandler)
	http.HandleFunc("/healthz", app.HealthHandler)

	addr := ":" + serverPort
	log.Printf("server listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
