# Band of Yeti Tracker

A web-based attendance tracker for F3 Denver that displays PAX post counts, leaderboards, and attendance logs across North and South regions. Built as a self-hosted replacement for the [Looker Studio dashboard](https://lookerstudio.google.com/embed/u/0/reporting/eafbae2b-f4b3-4e7a-a2ce-6753c95dedd3/page/c6IfF).

## Features

- **PAX Leaderboard** — ranked table with total posts, North/South breakdown, Q count, and AO list (sortable columns)
- **Attendance Log** — full record of every post with date, region, AO, PAX, Q, and active status
- **Summary Stats** — total posts, unique PAX, unique AOs, top PAX, and top Q
- **Filters** — date range, region (All / North / South), active AOs only

## Quick Start with Docker

# ask aussie, tackle for db creds
```bash
docker build -t yeti-tracker .

docker run -p 8080:8080 \
  -e DB_HOST=foo.amazonaws.com \
  -e DB_USER=changme \
  -e DB_PASSWORD=changeme \
  yeti-tracker
```

Or use an env file:

```bash
cp .env.example .env
# edit .env with your credentials
docker run -p 8080:8080 --env-file .env yeti-tracker
```

Then open [http://localhost:8080](http://localhost:8080).

## Quick Start without Docker

Requires Go 1.25+.

```bash
cp .env.example .env
# edit .env with your credentials
go run .
```

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `DB_HOST` | Yes | — | MySQL RDS hostname |
| `DB_USER` | Yes | — | Database username |
| `DB_PASSWORD` | Yes | — | Database password |
| `DB_PORT` | No | `3306` | MySQL port |
| `DB_NAME_NORTH` | No | `f3denver` | North region database name |
| `DB_NAME_SOUTH` | No | `f3denversouth` | South region database name |
| `DEFAULT_START_DATE` | No | `2025-12-21` | Default query start date (YYYY-MM-DD) |
| `DEFAULT_END_DATE` | No | `2026-03-20` | Default query end date (YYYY-MM-DD) |
| `SERVER_PORT` | No | `8080` | HTTP server port |

## Usage

On first load the dashboard uses the default date range from your env config. Use the filter bar at the top to adjust:

- **Start / End Date** — restrict the query window
- **Region** — All, North, or South
- **Active AOs Only** — exclude archived AOs

Click any column header in either table to sort.
