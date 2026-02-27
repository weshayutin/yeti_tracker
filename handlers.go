package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type CacheEntry struct {
	Data     PageData
	CachedAt time.Time
}

type Season struct {
	Label     string
	StartDate string
	EndDate   string
}

func historicalSeasons() []Season {
	return []Season{
		{Label: "2024/25 Season", StartDate: "2024-12-21", EndDate: "2025-03-20"},
		{Label: "2023/24 Season", StartDate: "2023-12-21", EndDate: "2024-03-20"},
	}
}

func isHistoricalSeason(startDate, endDate string) bool {
	for _, s := range historicalSeasons() {
		if s.StartDate == startDate && s.EndDate == endDate {
			return true
		}
	}
	return false
}

type App struct {
	DB               *sql.DB
	DBNorth          string
	DBSouth          string
	DefaultStartDate string
	DefaultEndDate   string
	CacheDir         string
	cache            map[string]CacheEntry
	cacheMu          sync.RWMutex
}

type AttendanceRow struct {
	Region   string
	Date     string
	AO       string
	PAX      string
	PAXId    string
	Q        string
	QId      string
	ActiveAO bool
}

type PAXStats struct {
	Rank        int
	PAX         string
	Total       int
	NorthCount  int
	SouthCount  int
	QCount      int
	AOs         []string
	IsYetiBeast bool
}

type PageData struct {
	Rows       []AttendanceRow
	Leaderboard []PAXStats
	TotalPosts int
	UniquePAX  int
	UniqueAOs  int
	TopPAX     string
	TopPAXCount int
	TopQ       string
	TopQCount  int
	StartDate  string
	EndDate    string
	Region     string
	ActiveOnly bool
	PAXFilter  string
	FromCache       bool
	CachedAt        string
	QueryTime       string
	PreviousSeasons []Season
}

func (app *App) buildQuery(startDate, endDate, region string, activeOnly bool) (string, []interface{}) {
	baseQuery := fmt.Sprintf(`
SELECT * FROM (
    SELECT
        'North' AS Region,
        bd.date AS Date,
        ao.ao AS AO,
        u.user_name AS PAX,
        u.user_id AS PAX_id,
        t1.Q AS Q,
        t1.user_id AS Q_id,
        NOT(ao.archived) AS ActiveAO
    FROM
        (%[1]s.aos ao
        JOIN %[1]s.users u)
        JOIN (%[1]s.bd_attendance bd
        JOIN (
            SELECT %[1]s.users.user_id AS user_id, %[1]s.users.user_name AS Q
            FROM %[1]s.users) t1 ON (bd.q_user_id = t1.user_id))
    WHERE
        (bd.ao_id = ao.channel_id)
        AND (u.user_id = bd.user_id)
        AND ao.AO LIKE 'ao_%%'
    UNION
    SELECT
        'South' AS Region,
        av.Date, av.AO, av.PAX, av.PAX_id, av.Q, av.Q_id,
        NOT(a.archived) AS ActiveAO
    FROM %[2]s.attendance_view av
    JOIN %[2]s.aos a ON a.ao = av.AO
    WHERE av.AO LIKE 'ao_%%'
) sub WHERE 1=1`, app.DBNorth, app.DBSouth)

	var conditions []string
	var args []interface{}

	if startDate != "" {
		conditions = append(conditions, "Date >= ?")
		args = append(args, startDate)
	}
	if endDate != "" {
		conditions = append(conditions, "Date <= ?")
		args = append(args, endDate)
	}
	if region != "" && region != "All" {
		conditions = append(conditions, "Region = ?")
		args = append(args, region)
	}
	if activeOnly {
		conditions = append(conditions, "ActiveAO = 1")
	}

	for _, c := range conditions {
		baseQuery += " AND " + c
	}

	baseQuery += " ORDER BY Date DESC"
	return baseQuery, args
}

func cacheKey(startDate, endDate, region, paxFilter string, activeOnly bool) string {
	active := "0"
	if activeOnly {
		active = "1"
	}
	return startDate + "|" + endDate + "|" + region + "|" + active + "|" + paxFilter
}

func (app *App) getCached(key string) (PageData, bool) {
	app.cacheMu.RLock()
	defer app.cacheMu.RUnlock()
	entry, ok := app.cache[key]
	if !ok {
		return PageData{}, false
	}
	data := entry.Data
	data.FromCache = true
	data.CachedAt = entry.CachedAt.Format("2006-01-02 15:04:05 MST")
	return data, true
}

func (app *App) setCache(key string, data PageData) {
	app.cacheMu.Lock()
	defer app.cacheMu.Unlock()
	app.cache[key] = CacheEntry{Data: data, CachedAt: time.Now()}
}

func (app *App) permanentCachePath(key string) string {
	hash := sha256.Sum256([]byte(key))
	return filepath.Join(app.CacheDir, hex.EncodeToString(hash[:8])+".json")
}

func (app *App) loadPermanentCache(key string) (PageData, bool) {
	path := app.permanentCachePath(key)
	b, err := os.ReadFile(path)
	if err != nil {
		return PageData{}, false
	}
	var data PageData
	if err := json.Unmarshal(b, &data); err != nil {
		log.Printf("permanent cache decode error for %s: %v", path, err)
		return PageData{}, false
	}
	log.Printf("serving from permanent cache: %s", path)
	return data, true
}

func (app *App) savePermanentCache(key string, data PageData) {
	path := app.permanentCachePath(key)
	b, err := json.Marshal(data)
	if err != nil {
		log.Printf("permanent cache encode error: %v", err)
		return
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		log.Printf("permanent cache write error for %s: %v", path, err)
		return
	}
	log.Printf("permanent cache saved: %s", path)
}

func (app *App) renderPage(w http.ResponseWriter, data PageData) {
	funcMap := template.FuncMap{
		"joinStrings": func(s []string, sep string) string {
			return strings.Join(s, sep)
		},
	}

	tmpl, err := template.New("index.html").Funcs(funcMap).ParseFiles("templates/index.html")
	if err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "Template rendering failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("template execute error: %v", err)
	}
}

func (app *App) IndexHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	region := r.URL.Query().Get("region")
	activeOnlyStr := r.URL.Query().Get("active_only")
	activeOnly := activeOnlyStr == "on" || activeOnlyStr == "true" || activeOnlyStr == "1"
	paxFilter := r.URL.Query().Get("pax_filter")

	if startDate == "" {
		startDate = app.DefaultStartDate
	}
	if endDate == "" {
		endDate = app.DefaultEndDate
	}
	if region == "" {
		region = "All"
	}

	key := cacheKey(startDate, endDate, region, paxFilter, activeOnly)
	historical := isHistoricalSeason(startDate, endDate)

	if historical {
		if cached, ok := app.loadPermanentCache(key); ok {
			cached.QueryTime = fmt.Sprintf("%.2fs (disk cache)", time.Since(start).Seconds())
			cached.PreviousSeasons = historicalSeasons()
			app.renderPage(w, cached)
			return
		}
	}

	query, args := app.buildQuery(startDate, endDate, region, activeOnly)

	var queryCtx context.Context
	var cancel context.CancelFunc
	if historical {
		queryCtx = r.Context()
	} else {
		queryCtx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
	}
	rows, err := app.DB.QueryContext(queryCtx, query, args...)
	if err != nil {
		log.Printf("query error: %v", err)
		if cached, ok := app.getCached(key); ok {
			log.Printf("serving cached data for key %q", key)
			cached.QueryTime = fmt.Sprintf("%.2fs (cached)", time.Since(start).Seconds())
			cached.PreviousSeasons = historicalSeasons()
			app.renderPage(w, cached)
			return
		}
		http.Error(w, "Database query failed and no cached data available", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var paxRegex *regexp.Regexp
	if paxFilter != "" {
		var err error
		paxRegex, err = regexp.Compile("(?i)" + paxFilter)
		if err != nil {
			log.Printf("invalid pax_filter regex %q: %v", paxFilter, err)
			paxRegex = nil
		}
	}

	var attendanceRows []AttendanceRow
	paxMap := make(map[string]*PAXStats)
	aoSet := make(map[string]bool)

	for rows.Next() {
		var row AttendanceRow
		var activeInt int
		if err := rows.Scan(&row.Region, &row.Date, &row.AO, &row.PAX, &row.PAXId, &row.Q, &row.QId, &activeInt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		row.ActiveAO = activeInt == 1

		if paxRegex != nil && !paxRegex.MatchString(row.PAX) {
			continue
		}

		attendanceRows = append(attendanceRows, row)

		aoSet[row.AO] = true

		stats, ok := paxMap[row.PAX]
		if !ok {
			stats = &PAXStats{PAX: row.PAX, AOs: []string{}}
			paxMap[row.PAX] = stats
		}
		stats.Total++
		if row.Region == "North" {
			stats.NorthCount++
		} else {
			stats.SouthCount++
		}
		if row.PAX == row.Q {
			stats.QCount++
		}
		found := false
		for _, a := range stats.AOs {
			if a == row.AO {
				found = true
				break
			}
		}
		if !found {
			stats.AOs = append(stats.AOs, row.AO)
		}
	}

	leaderboard := make([]PAXStats, 0, len(paxMap))
	for _, s := range paxMap {
		leaderboard = append(leaderboard, *s)
	}
	sort.Slice(leaderboard, func(i, j int) bool {
		return leaderboard[i].Total > leaderboard[j].Total
	})
	for i := range leaderboard {
		leaderboard[i].Rank = i + 1
		lb := &leaderboard[i]
		lb.IsYetiBeast = lb.Total >= 50 && len(lb.AOs) >= 10 && lb.QCount >= 6
	}

	topPAX := ""
	topPAXCount := 0
	if len(leaderboard) > 0 {
		topPAX = leaderboard[0].PAX
		topPAXCount = leaderboard[0].Total
	}

	topQ := ""
	topQCount := 0
	for _, s := range leaderboard {
		if s.QCount > topQCount {
			topQ = s.PAX
			topQCount = s.QCount
		}
	}

	data := PageData{
		Rows:            attendanceRows,
		Leaderboard:     leaderboard,
		TotalPosts:      len(attendanceRows),
		UniquePAX:       len(paxMap),
		UniqueAOs:       len(aoSet),
		TopPAX:          topPAX,
		TopPAXCount:     topPAXCount,
		TopQ:            topQ,
		TopQCount:       topQCount,
		StartDate:       startDate,
		EndDate:         endDate,
		Region:          region,
		ActiveOnly:      activeOnly,
		PAXFilter:       paxFilter,
		QueryTime:       fmt.Sprintf("%.2fs", time.Since(start).Seconds()),
		PreviousSeasons: historicalSeasons(),
	}

	app.setCache(key, data)
	log.Printf("cache updated for key %q (%d rows, %d PAX)", key, len(attendanceRows), len(paxMap))

	if historical {
		app.savePermanentCache(key, data)
	}

	app.renderPage(w, data)
}

func (app *App) PreWarmHistoricalCache() {
	for _, season := range historicalSeasons() {
		key := cacheKey(season.StartDate, season.EndDate, "All", "", false)
		if _, ok := app.loadPermanentCache(key); ok {
			log.Printf("pre-warm: cache hit for %s, skipping DB query", season.Label)
			continue
		}

		log.Printf("pre-warm: querying DB for %s (%s to %s)", season.Label, season.StartDate, season.EndDate)
		query, args := app.buildQuery(season.StartDate, season.EndDate, "All", false)
		rows, err := app.DB.Query(query, args...)
		if err != nil {
			log.Printf("pre-warm: query error for %s: %v", season.Label, err)
			continue
		}

		var attendanceRows []AttendanceRow
		paxMap := make(map[string]*PAXStats)
		aoSet := make(map[string]bool)

		for rows.Next() {
			var row AttendanceRow
			var activeInt int
			if err := rows.Scan(&row.Region, &row.Date, &row.AO, &row.PAX, &row.PAXId, &row.Q, &row.QId, &activeInt); err != nil {
				log.Printf("pre-warm: scan error: %v", err)
				continue
			}
			row.ActiveAO = activeInt == 1
			attendanceRows = append(attendanceRows, row)
			aoSet[row.AO] = true

			stats, ok := paxMap[row.PAX]
			if !ok {
				stats = &PAXStats{PAX: row.PAX, AOs: []string{}}
				paxMap[row.PAX] = stats
			}
			stats.Total++
			if row.Region == "North" {
				stats.NorthCount++
			} else {
				stats.SouthCount++
			}
			if row.PAX == row.Q {
				stats.QCount++
			}
			found := false
			for _, a := range stats.AOs {
				if a == row.AO {
					found = true
					break
				}
			}
			if !found {
				stats.AOs = append(stats.AOs, row.AO)
			}
		}
		rows.Close()

		leaderboard := make([]PAXStats, 0, len(paxMap))
		for _, s := range paxMap {
			leaderboard = append(leaderboard, *s)
		}
		sort.Slice(leaderboard, func(i, j int) bool {
			return leaderboard[i].Total > leaderboard[j].Total
		})
		for i := range leaderboard {
			leaderboard[i].Rank = i + 1
			lb := &leaderboard[i]
			lb.IsYetiBeast = lb.Total >= 50 && len(lb.AOs) >= 10 && lb.QCount >= 6
		}

		topPAX := ""
		topPAXCount := 0
		if len(leaderboard) > 0 {
			topPAX = leaderboard[0].PAX
			topPAXCount = leaderboard[0].Total
		}
		topQ := ""
		topQCount := 0
		for _, s := range leaderboard {
			if s.QCount > topQCount {
				topQ = s.PAX
				topQCount = s.QCount
			}
		}

		data := PageData{
			Rows:        attendanceRows,
			Leaderboard: leaderboard,
			TotalPosts:  len(attendanceRows),
			UniquePAX:   len(paxMap),
			UniqueAOs:   len(aoSet),
			TopPAX:      topPAX,
			TopPAXCount: topPAXCount,
			TopQ:        topQ,
			TopQCount:   topQCount,
			StartDate:   season.StartDate,
			EndDate:     season.EndDate,
			Region:      "All",
		}

		app.savePermanentCache(key, data)
		app.setCache(key, data)
		log.Printf("pre-warm: cached %s (%d rows, %d PAX)", season.Label, len(attendanceRows), len(paxMap))
	}
}

func (app *App) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (app *App) DBStatusHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	if err := app.DB.PingContext(ctx); err != nil {
		log.Printf("heartbeat: db DOWN (%v)", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"down"}`))
		return
	}
	log.Printf("heartbeat: db OK")
	w.Write([]byte(`{"status":"ok"}`))
}
