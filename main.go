package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(lrw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.String(), lrw.statusCode, time.Since(start).Truncate(time.Millisecond))
	})
}

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

	cacheDir := getEnv("CACHE_DIR", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Fatalf("failed to create cache directory %s: %v", cacheDir, err)
	}

	app := &App{
		DB:               db,
		DBNorth:          dbNorth,
		DBSouth:          dbSouth,
		DefaultStartDate: defaultStartDate,
		DefaultEndDate:   defaultEndDate,
		CacheDir:         cacheDir,
		cache:            make(map[string]CacheEntry),
	}

	go app.PreWarmHistoricalCache()

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/", app.IndexHandler)
	mux.HandleFunc("/healthz", app.HealthHandler)
	mux.HandleFunc("/api/dbstatus", app.DBStatusHandler)

	addr := ":" + serverPort
	log.Printf("server listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, loggingMiddleware(mux)))
}
