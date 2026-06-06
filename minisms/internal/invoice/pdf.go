// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package invoice

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phpdave11/gofpdf"
)

type PDFInput struct {
	InvoiceNumber string
	InvoiceDate   time.Time
	FromDate      time.Time
	ToDate        time.Time
	EntityName    string
	EntityLabel   string // "Client" or "Carrier"
	HeaderPath    string
	Summary       *Summary
}

func GeneratePDF(in PDFInput) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(10, 10, 10)
	pdf.SetAutoPageBreak(true, 12)
	pdf.AddPage()

	y := 10.0
	if path := strings.TrimSpace(in.HeaderPath); path != "" {
		if _, err := os.Stat(path); err == nil {
			const headerW = 210.0
			opt := gofpdf.ImageOptions{ImageType: imageTypeFor(path), ReadDpi: true}
			pdf.ImageOptions(path, 0, y, headerW, 0, false, opt, 0, "")
			if info := pdf.GetImageInfo(path); info != nil {
				y += headerW*info.Height()/info.Width() + 6
			} else {
				y += 30
			}
		}
	}
	pdf.SetY(y)

	pdf.SetFont("Arial", "B", 14)
	pdf.CellFormat(0, 8, "Invoice Summary", "", 1, "L", false, 0, "")
	pdf.Ln(2)
	pdf.SetFont("Arial", "", 10)

	summaryRows := [][]string{
		{"Invoice Number", in.InvoiceNumber},
		{"Invoice Date", in.InvoiceDate.Format("2006-01-02")},
		{"From Date", in.FromDate.Format("2006-01-02")},
		{"To Date", in.ToDate.Format("2006-01-02")},
		{in.EntityLabel + " Name", in.EntityName},
		{"Total Records", fmt.Sprintf("%d", in.Summary.TotalRecords)},
		{"Total Amount", in.Summary.TotalAmount + " " + in.Summary.Currency},
	}
	colW := []float64{55, 135}
	for _, row := range summaryRows {
		pdf.SetFont("Arial", "B", 10)
		pdf.CellFormat(colW[0], 7, row[0], "1", 0, "L", false, 0, "")
		pdf.SetFont("Arial", "", 10)
		pdf.CellFormat(colW[1], 7, row[1], "1", 1, "L", false, 0, "")
	}

	pdf.AddPage()
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(0, 8, "Details", "", 1, "L", false, 0, "")
	pdf.Ln(1)

	headers := []string{"DateTime", "To", "From", "Segments", "Cost"}
	widths := []float64{38, 42, 42, 22, 36}
	pdf.SetFont("Arial", "B", 9)
	for i, h := range headers {
		pdf.CellFormat(widths[i], 7, h, "1", 0, "L", false, 0, "")
	}
	pdf.Ln(-1)
	pdf.SetFont("Arial", "", 8)
	for _, ln := range in.Summary.Lines {
		vals := []string{
			ln.ReceivedAt.UTC().Format("2006-01-02 15:04"),
			truncate(ln.ToNumber, 22),
			truncate(ln.FromNumber, 22),
			fmt.Sprintf("%d", ln.Segments),
			ln.Cost,
		}
		for i, v := range vals {
			pdf.CellFormat(widths[i], 6, v, "1", 0, "L", false, 0, "")
		}
		pdf.Ln(-1)
	}
	if len(in.Summary.Lines) == 0 {
		pdf.CellFormat(180, 8, "No SMS records in selected period.", "1", 1, "C", false, 0, "")
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func WritePDFFile(dir, filename string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return "", err
	}
	return path, nil
}

func imageTypeFor(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "JPG"
	case ".png":
		return "PNG"
	default:
		return ""
	}
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}
