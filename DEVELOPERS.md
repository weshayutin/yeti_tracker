# Developer Guide

This document walks through the codebase and explains how to make common changes.

## Project Structure

```
yeti_tracker/
├── main.go              # Entry point: env var loading, DB connection, HTTP server
├── handlers.go          # Route handler, SQL query builder, data aggregation
├── templates/
│   └── index.html       # Single-page HTML/CSS/JS dashboard (Go template)
├── go.mod / go.sum      # Go module dependencies
├── Dockerfile           # Multi-stage container build
├── .dockerignore
├── .env.example         # Template for environment variables
└── README.md
```

### main.go

Reads environment variables (with `godotenv` for `.env` file support), builds a MySQL DSN, opens the connection pool, and starts an HTTP server. The `App` struct carries the DB handle and config to handlers.

### handlers.go

Contains all application logic in a single handler:

- `buildQuery()` — constructs the UNION SQL wrapped in a subquery, appending WHERE clauses based on filter params
- `IndexHandler()` — parses query string filters, runs the query, aggregates rows into a PAX leaderboard, and renders the template

Key types:

- `App` — holds `*sql.DB`, database names, and default dates
- `AttendanceRow` — one row from the UNION query
- `PAXStats` — aggregated stats per PAX (total, north, south, Q count, AO list)
- `PageData` — everything passed to the HTML template

### templates/index.html

A Go `html/template` file. Uses `{{.FieldName}}` for data binding and a custom `joinStrings` function registered in the `funcMap`. All CSS is inline in a `<style>` block; all JS is inline in a `<script>` block. No external dependencies.

## How the SQL Query Works

The query is a UNION of two SELECT statements:

1. **North**: Joins `f3denver.bd_attendance`, `f3denver.users`, and `f3denver.aos` to build attendance rows
2. **South**: Reads from `f3denversouth.attendance_view` (a pre-built view) joined with `f3denversouth.aos`

The UNION is wrapped in `SELECT * FROM (...) sub WHERE 1=1` so that date, region, and active filters can be appended uniformly as `AND` clauses with parameterized `?` placeholders.

## Local Development

### Without Docker

```bash
cp .env.example .env
# fill in DB_HOST, DB_USER, DB_PASSWORD
go run .
# open http://localhost:8080
```

Changes to `.go` files require restarting the server. Changes to `templates/index.html` take effect on the next page reload (the template is parsed on every request).

### With Docker

```bash
docker build -t yeti-tracker .
docker run -p 8080:8080 --env-file .env yeti-tracker
```

Rebuild the image after any code change.

## Common Changes

### Adding a New Filter

1. **handlers.go — `IndexHandler()`**: Read the new query param from `r.URL.Query().Get("my_filter")`.
2. **handlers.go — `buildQuery()`**: Accept the new param and append a SQL condition:
   ```go
   if myFilter != "" {
       conditions = append(conditions, "ColumnName = ?")
       args = append(args, myFilter)
   }
   ```
3. **handlers.go — `PageData`**: Add a field so the template can reflect the current value.
4. **templates/index.html**: Add an `<input>` or `<select>` inside the `<form class="filters">` block with `name="my_filter"`.

### Adding a New Stat Card

1. **handlers.go — `PageData`**: Add a new field (e.g., `MyMetric int`).
2. **handlers.go — `IndexHandler()`**: Compute the value during the row iteration loop or after it.
3. **templates/index.html**: Add a new `<div class="stat-card">` block inside `<div class="stats">`:
   ```html
   <div class="stat-card">
       <div class="stat-label">My Metric</div>
       <div class="stat-value">{{.MyMetric}}</div>
   </div>
   ```

### Adding a New Leaderboard Column

1. **handlers.go — `PAXStats`**: Add a field (e.g., `MyCount int`).
2. **handlers.go — `IndexHandler()`**: Increment it in the per-row aggregation loop.
3. **templates/index.html**: Add a `<th>` in the leaderboard thead and a `<td>` in the tbody `{{range}}` block:
   ```html
   <th onclick="sortTable('leaderboard-table', N, 'num')">My Col <span class="sort-arrow"></span></th>
   ```
   ```html
   <td>{{.MyCount}}</td>
   ```
   Update the column index `N` for proper sort targeting.

### Modifying the SQL Query

Edit `buildQuery()` in `handlers.go`. The raw SQL uses `%[1]s` / `%[2]s` format verbs for the north/south database names. Use `%%` to escape literal `%` characters in LIKE clauses. Filter values use `?` parameterized placeholders — never interpolate user input directly.

### Adding a New Template Function

Register it in the `funcMap` inside `IndexHandler()`:

```go
funcMap := template.FuncMap{
    "joinStrings": func(s []string, sep string) string {
        return strings.Join(s, sep)
    },
    "myFunc": func(args...) returnType {
        // ...
    },
}
```

Then use it in the template as `{{myFunc .SomeField "arg"}}`.

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/go-sql-driver/mysql` | MySQL driver for `database/sql` |
| `github.com/joho/godotenv` | Loads `.env` file into environment |

Add new dependencies with `go get`:

```bash
go get github.com/some/package@latest
```
