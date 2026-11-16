package reports

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

// CSVFile is a query result as a CSV attachment.
type CSVFile struct {
	Data []byte
	// Rows is the rows the file holds, TotalRows the rows of the result.
	Rows      int
	TotalRows int
	// HeaderTooLarge means the header row alone passes the cap, so there
	// is no file to attach.
	HeaderTooLarge bool
}

// Cut reports a file that holds fewer rows than the result.
func (f CSVFile) Cut() bool { return f.Rows < f.TotalRows }

// BuildCSV writes the header and the rows of res, with the cells the log
// export writes. A file past maxBytes ends at the last whole row that fits.
func BuildCSV(res *models.QueryResult, maxBytes int) (CSVFile, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		header[i] = c.Name
	}
	if err := writeCSVRecord(w, header); err != nil {
		return CSVFile{}, err
	}
	file := CSVFile{TotalRows: len(res.Logs)}
	if buf.Len() > maxBytes {
		file.HeaderTooLarge = true
		return file, nil
	}
	record := make([]string, len(res.Columns))
	for _, row := range res.Logs {
		for i, c := range res.Columns {
			record[i] = util.CSVValue(row[c.Name])
		}
		before := buf.Len()
		if err := writeCSVRecord(w, record); err != nil {
			return CSVFile{}, err
		}
		if buf.Len() > maxBytes {
			buf.Truncate(before)
			break
		}
		file.Rows++
	}
	file.Data = buf.Bytes()
	return file, nil
}

func writeCSVRecord(w *csv.Writer, record []string) error {
	if err := w.Write(record); err != nil {
		return fmt.Errorf("writing a csv row: %w", err)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("writing a csv row: %w", err)
	}
	return nil
}

// maxCSVNameRunes bounds the report name in a file name. RFC 2231 encodes
// a non-ASCII character in up to 12 bytes, and the Content-Disposition line
// must stay under SMTP's 998 bytes.
const maxCSVNameRunes = 40

// CSVName names the attachment after the report and the run's date in
// loc, with anything but letters, digits, '-' and '_' turned into '-', and
// the name cut to maxCSVNameRunes characters.
func CSVName(reportName string, at time.Time, loc *time.Location) string {
	if r := []rune(reportName); len(r) > maxCSVNameRunes {
		reportName = string(r[:maxCSVNameRunes])
	}
	name := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, reportName)
	return fmt.Sprintf("%s-%s.csv", name, at.In(loc).Format(time.DateOnly))
}

// CSVNote is the line the email prints about its attachment.
func CSVNote(f CSVFile) string {
	if f.HeaderTooLarge {
		return "The CSV file was left out: its header row alone is over the attachment size limit."
	}
	if f.Cut() {
		return fmt.Sprintf("The attached CSV holds the first %d of %d rows; the rest did not fit the attachment size limit.", f.Rows, f.TotalRows)
	}
	return fmt.Sprintf("The attached CSV holds %d rows.", f.Rows)
}
