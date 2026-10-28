package inspect

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

//go:embed template.html
var page string

const dataMarker = "/*__CATALOG_DATA__*/null"

// Write renders the page for d to w: template.html with d inlined as JSON
// (encoding/json escapes <, > and &, so it is safe inside <script>). The
// page needs no network and works from file://.
func Write(d *Data, w io.Writer) error {
	js, err := json.Marshal(d)
	if err != nil {
		return err
	}
	i := strings.Index(page, dataMarker)
	if i < 0 {
		return errors.New("inspect: template has no data marker")
	}
	if _, err := io.WriteString(w, page[:i]); err != nil {
		return err
	}
	if _, err := w.Write(js); err != nil {
		return err
	}
	_, err = io.WriteString(w, page[i+len(dataMarker):])
	return err
}
