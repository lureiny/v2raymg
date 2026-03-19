package store

import (
	"encoding/json"
	"fmt"
)

// InboundRecord holds persistent data for a single inbound.
type InboundRecord struct {
	Tag           string
	ContainerType string
	CertSource    string
	CertDomain    string
	NativeJSON    []byte // stored as formatted JSON bytes
}

// InboundStore abstracts inbound persistence.
type InboundStore interface {
	// Load returns all persisted inbound records.
	Load() ([]*InboundRecord, error)
	// Save upserts an inbound record.
	Save(rec *InboundRecord) error
	// Delete removes the inbound record with the given tag.
	Delete(tag string) error
}

// SQLiteInboundStore implements InboundStore using SQLite.
type SQLiteInboundStore struct {
	db *DB
}

// NewSQLiteInboundStore creates a new SQLiteInboundStore backed by db.
func NewSQLiteInboundStore(db *DB) *SQLiteInboundStore {
	return &SQLiteInboundStore{db: db}
}

// Save upserts an inbound record. NativeJSON is pretty-printed (2-space indent) before writing.
func (s *SQLiteInboundStore) Save(rec *InboundRecord) error {
	pretty, err := prettyPrintJSON(rec.NativeJSON)
	if err != nil {
		return fmt.Errorf("inbound_store.Save: format native_json: %w", err)
	}

	_, err = s.db.DB().Exec(`
		INSERT OR REPLACE INTO inbounds (tag, container_type, cert_source, cert_domain, native_json, updated_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))
	`, rec.Tag, rec.ContainerType, rec.CertSource, rec.CertDomain, string(pretty))
	if err != nil {
		return fmt.Errorf("inbound_store.Save: %w", err)
	}
	return nil
}

// Load returns all stored inbound records.
func (s *SQLiteInboundStore) Load() ([]*InboundRecord, error) {
	rows, err := s.db.DB().Query(
		`SELECT tag, container_type, cert_source, cert_domain, native_json FROM inbounds`,
	)
	if err != nil {
		return nil, fmt.Errorf("inbound_store.Load: %w", err)
	}
	defer rows.Close()

	var records []*InboundRecord
	for rows.Next() {
		var r InboundRecord
		var nativeStr string
		if err := rows.Scan(&r.Tag, &r.ContainerType, &r.CertSource, &r.CertDomain, &nativeStr); err != nil {
			return nil, fmt.Errorf("inbound_store.Load scan: %w", err)
		}
		r.NativeJSON = []byte(nativeStr)
		records = append(records, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inbound_store.Load rows: %w", err)
	}
	return records, nil
}

// Delete removes the inbound record with the given tag.
func (s *SQLiteInboundStore) Delete(tag string) error {
	_, err := s.db.DB().Exec(`DELETE FROM inbounds WHERE tag = ?`, tag)
	if err != nil {
		return fmt.Errorf("inbound_store.Delete: %w", err)
	}
	return nil
}

// prettyPrintJSON re-formats raw JSON with two-space indentation for human readability.
func prettyPrintJSON(raw []byte) ([]byte, error) {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", "  ")
}
