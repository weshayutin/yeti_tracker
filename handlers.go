package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

type App struct {
	DB               *sql.DB
	DBNorth          string
	DBSouth          string
	DefaultStartDate string
	DefaultEndDate   string
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
	Rank       int
	PAX        string
	Total      int
	NorthCount int
	SouthCount int
	QCount     int
	AOs        []string
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
	QueryTime  string
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

func (app *App) IndexHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	region := r.URL.Query().Get("region")
	activeOnlyStr := r.URL.Query().Get("active_only")
	activeOnly := activeOnlyStr == "on" || activeOnlyStr == "true" || activeOnlyStr == "1"

	if startDate == "" {
		startDate = app.DefaultStartDate
	}
	if endDate == "" {
		endDate = app.DefaultEndDate
	}
	if region == "" {
		region = "All"
	}

	query, args := app.buildQuery(startDate, endDate, region, activeOnly)

	rows, err := app.DB.Query(query, args...)
	if err != nil {
		log.Printf("query error: %v", err)
		http.Error(w, "Database query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var attendanceRows []AttendanceRow
	paxMap := make(map[string]*PAXStats)
	qCountMap := make(map[string]int)
	aoSet := make(map[string]bool)

	for rows.Next() {
		var row AttendanceRow
		var activeInt int
		if err := rows.Scan(&row.Region, &row.Date, &row.AO, &row.PAX, &row.PAXId, &row.Q, &row.QId, &activeInt); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}
		row.ActiveAO = activeInt == 1
		attendanceRows = append(attendanceRows, row)

		aoSet[row.AO] = true
		qCountMap[row.Q]++

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
	}

	topPAX := ""
	topPAXCount := 0
	if len(leaderboard) > 0 {
		topPAX = leaderboard[0].PAX
		topPAXCount = leaderboard[0].Total
	}

	topQ := ""
	topQCount := 0
	for name, count := range qCountMap {
		if count > topQCount {
			topQ = name
			topQCount = count
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
		StartDate:   startDate,
		EndDate:     endDate,
		Region:      region,
		ActiveOnly:  activeOnly,
		QueryTime:   fmt.Sprintf("%.2fs", time.Since(start).Seconds()),
	}

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
