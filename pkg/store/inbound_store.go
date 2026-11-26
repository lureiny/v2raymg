package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// InboundRecord holds persistent data for a single inbound.
type InboundRecord struct {
	Tag              string
	ContainerType    string
	CertSource       string
	CertDomain       string
	NativeJSON       []byte // stored as formatted JSON bytes
	InboundType      string
	AccessTags       []string
	ChainOutboundURI string
	Remark           string
}

// InboundStore abstracts inbound persistence.
type InboundStore interface {
	// Load returns all persisted inbound records.
	Load() ([]*InboundRecord, error)
	// Get returns a single inbound record by tag, or (nil, nil) if not found.
	Get(tag string) (*InboundRecord, error)
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

const selectInboundColumns = `tag, container_type, cert_source, cert_domain, native_json, inbound_type, access_tags, chain_outbound_uri, remark`

// Save upserts an inbound record. NativeJSON is pretty-printed (2-space indent) before writing.
func (s *SQLiteInboundStore) Save(rec *InboundRecord) error {
	pretty, err := prettyPrintJSON(rec.NativeJSON)
	if err != nil {
		return fmt.Errorf("inbound_store.Save: format native_json: %w", err)
	}

	inboundType := rec.InboundType
	if inboundType == "" {
		inboundType = "direct"
	}

	accessTagsJSON := "[]"
	if len(rec.AccessTags) > 0 {
		data, err := json.Marshal(rec.AccessTags)
		if err != nil {
			return fmt.Errorf("inbound_store.Save: marshal access_tags: %w", err)
		}
		accessTagsJSON = string(data)
	}

	_, err = s.db.DB().Exec(`
		INSERT OR REPLACE INTO inbounds (
			tag, container_type, cert_source, cert_domain, native_json,
			inbound_type, access_tags, chain_outbound_uri, remark, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, rec.Tag, rec.ContainerType, rec.CertSource, rec.CertDomain, string(pretty),
		inboundType, accessTagsJSON, rec.ChainOutboundURI, rec.Remark)
	if err != nil {
		return fmt.Errorf("inbound_store.Save: %w", err)
	}
	return nil
}

// Load returns all stored inbound records.
func (s *SQLiteInboundStore) Load() ([]*InboundRecord, error) {
	rows, err := s.db.DB().Query(`SELECT ` + selectInboundColumns + ` FROM inbounds`)
	if err != nil {
		return nil, fmt.Errorf("inbound_store.Load: %w", err)
	}
	defer rows.Close()

	var records []*InboundRecord
	for rows.Next() {
		r, err := scanInboundRow(rows)
		if err != nil {
			return nil, fmt.Errorf("inbound_store.Load scan: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inbound_store.Load rows: %w", err)
	}
	return records, nil
}

// Get returns a single stored inbound record by tag, or (nil, nil) if not found.
func (s *SQLiteInboundStore) Get(tag string) (*InboundRecord, error) {
	row := s.db.DB().QueryRow(`SELECT `+selectInboundColumns+` FROM inbounds WHERE tag = ?`, tag)
	r, err := scanInboundRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("inbound_store.Get: %w", err)
	}
	return r, nil
}

func scanInboundRow(scanner interface{ Scan(dest ...any) error }) (*InboundRecord, error) {
	var r InboundRecord
	var nativeStr, accessTagsStr string
	if err := scanner.Scan(
		&r.Tag, &r.ContainerType, &r.CertSource, &r.CertDomain, &nativeStr,
		&r.InboundType, &accessTagsStr, &r.ChainOutboundURI, &r.Remark,
	); err != nil {
		return nil, err
	}
	r.NativeJSON = []byte(nativeStr)
	if r.InboundType == "" {
		r.InboundType = "direct"
	}
	if accessTagsStr != "" && accessTagsStr != "[]" {
		if accessTagsStr == "null" {
			return nil, fmt.Errorf("corrupt access_tags for inbound %q: null is not a valid tag list", r.Tag)
		}
		if err := json.Unmarshal([]byte(accessTagsStr), &r.AccessTags); err != nil {
			return nil, fmt.Errorf("corrupt access_tags for inbound %q: %w", r.Tag, err)
		}
	}
	return &r, nil
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
