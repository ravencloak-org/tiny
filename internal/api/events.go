package api

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/tinyraven/tinyraven/internal/model"
)

// handleEvents ingests JSON / NDJSON event rows for a datasource (ADRs 0004,
// 0018, 0023). Requires ?name=<datasource>. Always returns 202 with row counts,
// never rejecting the batch for individual bad rows.
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing required query parameter: name")
		return
	}

	body := http.MaxBytesReader(w, r.Body, s.deps.MaxCompressedBytes)
	reader, err := decodeBody(body, r.Header.Get("Content-Encoding"))
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, err.Error())
		return
	}
	defer reader.Close()

	rows, err := parseRows(reader)
	if err != nil {
		// Body too large surfaces here as a read error -> 413.
		if _, ok := err.(*http.MaxBytesError); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "malformed request body: "+err.Error())
		return
	}

	ok, quarantined, err := s.deps.Ingester.Ingest(r.Context(), name, rows)
	if err != nil {
		if errors.Is(err, model.ErrUnknownDatasource) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.deps.IngestObserver != nil {
		s.deps.IngestObserver(ok, quarantined)
	}
	encodeJSON(w, http.StatusAccepted, map[string]int{
		"successful_rows":  ok,
		"quarantined_rows": quarantined,
	})
}

// decodeBody wraps the reader to transparently decompress gzip (ADR 0023).
// zstd support lands with the klauspost/compress dep; identity passes through.
func decodeBody(r io.Reader, encoding string) (io.ReadCloser, error) {
	switch encoding {
	case "", "identity":
		return io.NopCloser(r), nil
	case "gzip":
		return gzip.NewReader(r)
	default:
		// ponytail: zstd deferred until a producer needs it; add klauspost then.
		return nil, errUnsupportedEncoding(encoding)
	}
}

type errUnsupportedEncoding string

func (e errUnsupportedEncoding) Error() string {
	return "unsupported Content-Encoding: " + string(e)
}

// parseRows accepts a JSON array, a single JSON object, or NDJSON (one object
// per line) and returns the rows as raw JSON, preserving order.
func parseRows(r io.Reader) ([]json.RawMessage, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var rows []json.RawMessage
		if err := json.Unmarshal(trimmed, &rows); err != nil {
			return nil, err
		}
		return rows, nil
	}
	// NDJSON (also covers a single object on one line).
	var rows []json.RawMessage
	sc := bufio.NewScanner(bytes.NewReader(trimmed))
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20) // allow large single rows
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		rows = append(rows, append(json.RawMessage(nil), line...))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}
