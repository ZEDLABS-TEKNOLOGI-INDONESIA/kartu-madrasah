package csvutil

import (
	"bytes"
	"encoding/csv"
	"io"
	"strings"
)

func Parse(data []byte) ([]map[string]string, []string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true

	headers, err := r.Read()
	if err != nil {
		return nil, nil, err
	}
	for i, h := range headers {
		headers[i] = strings.TrimSpace(h)
	}

	var rows []map[string]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		row := make(map[string]string)
		for i, h := range headers {
			if i < len(rec) {
				row[h] = strings.TrimSpace(rec[i])
			}
		}
		rows = append(rows, row)
	}

	return rows, headers, nil
}
