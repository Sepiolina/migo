// Package sqlserver is the shared, lazily opened SQL Server connection pool.
// Any module that needs the database takes a *Pool; none of them opens its
// own connection.
package sqlserver

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sync"
	"time"

	mssql "github.com/microsoft/go-mssqldb"
)

// VarChar types a query parameter as VARCHAR (not NVARCHAR), so SQL Server
// can use the index on a VARCHAR column.
type VarChar = mssql.VarChar

const idleCloseAfter = 5 * time.Minute // pool connections close after 5 min unused

// Config is the connection setting (the "db_*" keys of config.json).
type Config struct {
	Server, User, Password, Database string
	Port                             int

	// TLSMin is the lowest TLS version accepted for the login handshake:
	// "1.0", "1.1", "1.2" or "1.3". Empty means "1.0", because older SQL
	// Server builds only speak TLS 1.0/1.1 and Go refuses those by default
	// ("server selected unsupported protocol version 301").
	TLSMin string
}

// Pool opens the connection on first use and drops idle connections after
// five minutes. The service handles a handful of uploads a week, so nothing
// should stay connected in between.
type Pool struct {
	cfg Config
	mu  sync.Mutex
	db  *sql.DB
}

// New creates a pool; it does not connect yet.
func New(cfg Config) *Pool { return &Pool{cfg: cfg} }

// DB returns the shared *sql.DB, opening it on first call.
func (p *Pool) DB() (*sql.DB, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.db != nil {
		return p.db, nil
	}
	u := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(p.cfg.User, p.cfg.Password),
		Host:   fmt.Sprintf("%s:%d", p.cfg.Server, p.cfg.Port),
	}
	q := url.Values{}
	q.Set("database", p.cfg.Database)
	q.Set("encrypt", "false") // login packet only, like tedious' encrypt:false
	q.Set("TrustServerCertificate", "true")
	tlsMin := p.cfg.TLSMin
	if tlsMin == "" {
		tlsMin = "1.0"
	}
	q.Set("tlsmin", tlsMin)
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlserver", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(idleCloseAfter)
	p.db = db
	return db, nil
}

// Close releases the pool (called on shutdown).
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.db == nil {
		return nil
	}
	err := p.db.Close()
	p.db = nil
	return err
}

// Query runs query with positional parameters (@p1, @p2...) and returns the
// column names and rows. Text parameters are sent as VARCHAR; values come
// back as string, int64, float64, bool, time.Time or nil.
func (p *Pool) Query(ctx context.Context, query string, args []any) ([]string, [][]any, error) {
	db, err := p.DB()
	if err != nil {
		return nil, nil, err
	}
	params := make([]any, len(args))
	for i, a := range args {
		if s, ok := a.(string); ok {
			params[i] = VarChar(s)
		} else {
			params[i] = a
		}
	}
	rs, err := db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, nil, err
	}
	defer rs.Close()
	cols, err := rs.Columns()
	if err != nil {
		return nil, nil, err
	}
	var rows [][]any
	for rs.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		rows = append(rows, vals)
	}
	return cols, rows, rs.Err()
}
