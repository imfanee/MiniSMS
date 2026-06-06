// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/invoice"
	"github.com/minisms/minisms/internal/pathutil"
)

const invoicePageSize = 20

type invoicePanelData struct {
	EntityType   string
	EntityID     string
	EntityName   string
	CSRFToken    string
	FromDate     string
	ToDate       string
	ShowNewForm  bool
	Rows            []invoiceRowView
	Total           int
	PendingCount    int
	UnpaidAmountFmt string
	Page            int
	PageCount    int
	PrevPage     int
	NextPage     int
	Errors       map[string]string
	BasePath     string
}

type invoiceRowView struct {
	InvoiceID     string
	InvoiceNumber string
	InvoiceDate   string
	FromDate      string
	ToDate        string
	TotalAmount   string
	PendingAmount string
	Currency      string
	Status        string
	StatusLabel   string
	PDFURL        string
}

func (h *Handlers) dataDir() string {
	return "."
}

func (h *Handlers) invoiceAssetsDir() string {
	return filepath.Join(h.dataDir(), "assets")
}

func (h *Handlers) invoiceHeaderPath(ctx context.Context) string {
	rel := strings.TrimSpace(db.Setting(ctx, h.Pool, "invoice_header_image", "assets/invoice_header.png"))
	p, err := pathutil.ResolveUnder(h.dataDir(), rel)
	if err != nil {
		return ""
	}
	return p
}

func (h *Handlers) invoiceStorageDir(entityType, entityID string) string {
	return filepath.Join(h.dataDir(), "invoices", entityType, entityID)
}

func defaultInvoiceDates() (string, string) {
	from, to := invoice.DefaultPeriod(time.Now().UTC())
	return from.Format("2006-01-02"), to.Format("2006-01-02")
}

func (h *Handlers) GetClientInvoicesPanel() http.HandlerFunc {
	return h.invoicePanelHandler(db.InvoiceEntityClient, "invoices_panel")
}

func (h *Handlers) GetCarrierInvoicesPanel() http.HandlerFunc {
	return h.invoicePanelHandler(db.InvoiceEntityCarrier, "invoices_panel")
}

func (h *Handlers) invoicePanelHandler(entityType, templateName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entityID := chi.URLParam(r, "id")
		entityName, err := h.invoiceEntityName(r, entityType, entityID)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		page := parseIntDefault(r.URL.Query().Get("page"), 1)
		stats, err := db.GetInvoiceStats(r.Context(), h.Pool, entityType, entityID)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rows, total, err := db.ListInvoices(r.Context(), h.Pool, entityType, entityID, page, invoicePageSize)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		pageCount := (total + invoicePageSize - 1) / invoicePageSize
		if pageCount == 0 {
			pageCount = 1
		}
		fromDef, toDef := defaultInvoiceDates()
		base := h.invoiceBasePath(entityType, entityID)
		d := invoicePanelData{
			EntityType: entityType, EntityID: entityID, EntityName: entityName,
			CSRFToken: csrf.Token(r), FromDate: fromDef, ToDate: toDef,
			ShowNewForm: r.URL.Query().Get("new") == "1",
			Total: total, PendingCount: stats.PendingCount,
			UnpaidAmountFmt: FormatBalance2dp(stats.UnpaidAmount, stats.Currency),
			Page: page, PageCount: pageCount,
			PrevPage: max(1, page-1), NextPage: min(pageCount, page+1),
			BasePath: base,
		}
		fragT := h.CLIFragT
		if entityType == db.InvoiceEntityCarrier {
			fragT = h.CarrFragT
		}
		for _, row := range rows {
			d.Rows = append(d.Rows, invoiceRowView{
				InvoiceID: row.InvoiceID, InvoiceNumber: row.InvoiceNumber,
				InvoiceDate: row.InvoiceDate.Format("2006-01-02"),
				FromDate: row.FromDate.Format("2006-01-02"), ToDate: row.ToDate.Format("2006-01-02"),
				TotalAmount: row.TotalAmount, PendingAmount: row.PendingAmount, Currency: row.Currency,
				Status: row.Status, StatusLabel: db.InvoiceStatusLabel(row.Status),
				PDFURL: fmt.Sprintf("%s/%s/pdf", base, row.InvoiceID),
			})
		}
		if err := execT(w, fragT, templateName, d); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) invoiceBasePath(entityType, entityID string) string {
	if entityType == db.InvoiceEntityCarrier {
		return "/admin/carriers/" + entityID + "/invoices"
	}
	return "/admin/clients/" + entityID + "/invoices"
}

func (h *Handlers) invoiceEntityName(r *http.Request, entityType, entityID string) (string, error) {
	if entityType == db.InvoiceEntityCarrier {
		var name string
		err := h.Pool.QueryRow(r.Context(), `SELECT name FROM carriers WHERE carrier_id=$1::uuid`, entityID).Scan(&name)
		return name, err
	}
	var name string
	err := h.Pool.QueryRow(r.Context(), `SELECT name FROM clients WHERE client_id=$1::uuid`, entityID).Scan(&name)
	return name, err
}

func (h *Handlers) PreviewClientInvoice() http.HandlerFunc {
	return h.previewInvoice(db.InvoiceEntityClient, "Client")
}

func (h *Handlers) PreviewCarrierInvoice() http.HandlerFunc {
	return h.previewInvoice(db.InvoiceEntityCarrier, "Carrier")
}

func (h *Handlers) previewInvoice(entityType, entityLabel string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entityID := chi.URLParam(r, "id")
		_ = r.ParseForm()
		from, to, errs := parseInvoiceDates(r)
		if len(errs) > 0 {
			h.renderInvoicePanelError(w, r, entityType, entityID, from.Format("2006-01-02"), to.Format("2006-01-02"), errs, true)
			return
		}
		pdf, filename, err := h.buildInvoicePDF(r, entityType, entityLabel, entityID, from, to, "PREVIEW")
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		_, _ = w.Write(pdf)
	}
}

func (h *Handlers) GenerateClientInvoice() http.HandlerFunc {
	return h.generateInvoice(db.InvoiceEntityClient, "Client")
}

func (h *Handlers) GenerateCarrierInvoice() http.HandlerFunc {
	return h.generateInvoice(db.InvoiceEntityCarrier, "Carrier")
}

func (h *Handlers) generateInvoice(entityType, entityLabel string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entityID := chi.URLParam(r, "id")
		_ = r.ParseForm()
		from, to, errs := parseInvoiceDates(r)
		if len(errs) > 0 {
			h.renderInvoicePanelError(w, r, entityType, entityID, from.Format("2006-01-02"), to.Format("2006-01-02"), errs, true)
			return
		}
		num, err := db.NextInvoiceNumber(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		pdfBytes, _, err := h.buildInvoicePDF(r, entityType, entityLabel, entityID, from, to, num)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		filename := num + ".pdf"
		dir := h.invoiceStorageDir(entityType, entityID)
		_, err = invoice.WritePDFFile(dir, filename, pdfBytes)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		summary, err := h.loadInvoiceSummary(r, entityType, entityID, from, to)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		relPath := filepath.Join("invoices", entityType, entityID, filename)
		_, err = db.CreateInvoice(r.Context(), h.Pool, db.CreateInvoiceParams{
			InvoiceNumber: num,
			EntityType: entityType, EntityID: entityID,
			FromDate: from, ToDate: to,
			TotalRecords: summary.TotalRecords, TotalAmount: summary.TotalAmount,
			Currency: summary.Currency, PDFPath: relPath,
		})
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.recordAudit(r, "invoice.create", entityType, &entityID, nil, map[string]string{
			"from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"),
		})
		h.invoicePanelHandler(entityType, "invoices_panel").ServeHTTP(w, r)
	}
}

func (h *Handlers) DownloadClientInvoicePDF() http.HandlerFunc {
	return h.downloadInvoicePDF(db.InvoiceEntityClient)
}

func (h *Handlers) DownloadCarrierInvoicePDF() http.HandlerFunc {
	return h.downloadInvoicePDF(db.InvoiceEntityCarrier)
}

func (h *Handlers) downloadInvoicePDF(entityType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entityID := chi.URLParam(r, "id")
		invoiceID := chi.URLParam(r, "invoice_id")
		inv, err := db.GetInvoice(r.Context(), h.Pool, entityType, entityID, invoiceID)
		if err != nil {
			if db.IsNotFoundInvoice(err) {
				http.NotFound(w, r)
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		path, err := pathutil.ResolveUnder(h.dataDir(), inv.PDFPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+inv.InvoiceNumber+`.pdf"`)
		_, _ = w.Write(data)
	}
}

func (h *Handlers) buildInvoicePDF(r *http.Request, entityType, entityLabel, entityID string, from, to time.Time, invoiceNumber string) ([]byte, string, error) {
	entityName, err := h.invoiceEntityName(r, entityType, entityID)
	if err != nil {
		return nil, "", err
	}
	summary, err := h.loadInvoiceSummary(r, entityType, entityID, from, to)
	if err != nil {
		return nil, "", err
	}
	if invoiceNumber == "" {
		invoiceNumber = "PREVIEW"
	}
	pdf, err := invoice.GeneratePDF(invoice.PDFInput{
		InvoiceNumber: invoiceNumber,
		InvoiceDate:   time.Now().UTC(),
		FromDate:      from, ToDate: to,
		EntityName: entityName, EntityLabel: entityLabel,
		HeaderPath: h.invoiceHeaderPath(r.Context()),
		Summary:    summary,
	})
	if err != nil {
		return nil, "", err
	}
	filename := invoiceNumber + ".pdf"
	return pdf, filename, nil
}

func (h *Handlers) loadInvoiceSummary(r *http.Request, entityType, entityID string, from, to time.Time) (*invoice.Summary, error) {
	if entityType == db.InvoiceEntityCarrier {
		return invoice.LoadCarrierLines(r.Context(), h.Pool, entityID, from, to)
	}
	return invoice.LoadClientLines(r.Context(), h.Pool, entityID, from, to)
}

func parseInvoiceDates(r *http.Request) (time.Time, time.Time, map[string]string) {
	errs := map[string]string{}
	fromStr := strings.TrimSpace(r.FormValue("from_date"))
	toStr := strings.TrimSpace(r.FormValue("to_date"))
	if fromStr == "" || toStr == "" {
		defFrom, defTo := defaultInvoiceDates()
		if fromStr == "" {
			fromStr = defFrom
		}
		if toStr == "" {
			toStr = defTo
		}
	}
	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		errs["from_date"] = "Invalid from date"
	}
	to, err2 := time.Parse("2006-01-02", toStr)
	if err2 != nil {
		errs["to_date"] = "Invalid to date"
	}
	if len(errs) == 0 && from.After(to) {
		errs["to_date"] = "To date must be on or after from date"
	}
	return from, to, errs
}

func (h *Handlers) renderInvoicePanelError(w http.ResponseWriter, r *http.Request, entityType, entityID, from, to string, errs map[string]string, showForm bool) {
	entityName, _ := h.invoiceEntityName(r, entityType, entityID)
	stats, _ := db.GetInvoiceStats(r.Context(), h.Pool, entityType, entityID)
	d := invoicePanelData{
		EntityType: entityType, EntityID: entityID, EntityName: entityName,
		CSRFToken: csrf.Token(r), FromDate: from, ToDate: to,
		ShowNewForm: showForm, Errors: errs, BasePath: h.invoiceBasePath(entityType, entityID),
		Total: stats.TotalCount, PendingCount: stats.PendingCount,
		UnpaidAmountFmt: FormatBalance2dp(stats.UnpaidAmount, stats.Currency),
	}
	fragT := h.CLIFragT
	if entityType == db.InvoiceEntityCarrier {
		fragT = h.CarrFragT
	}
	_ = execT(w, fragT, "invoices_panel", d)
}

func (h *Handlers) UploadInvoiceHeader() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		file, hdr, err := r.FormFile("header_image")
		if err != nil {
			http.Error(w, "header_image file required", http.StatusBadRequest)
			return
		}
		defer file.Close()
		ext := strings.ToLower(filepath.Ext(hdr.Filename))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
			http.Error(w, "only PNG or JPEG allowed", http.StatusBadRequest)
			return
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(file, head)
		head = head[:n]
		mime := http.DetectContentType(head)
		if mime != "image/png" && mime != "image/jpeg" {
			http.Error(w, "file must be PNG or JPEG image data", http.StatusBadRequest)
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if err := os.MkdirAll(h.invoiceAssetsDir(), 0o750); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		destName := "invoice_header" + ext
		destPath := filepath.Join(h.invoiceAssetsDir(), destName)
		out, err := os.Create(destPath)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_, err = io.Copy(out, file)
		_ = out.Close()
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rel := filepath.Join("assets", destName)
		if err := pathutil.ValidateRelativeDataPath(rel, "assets"); err != nil {
			http.Error(w, "invalid storage path", http.StatusBadRequest)
			return
		}
		_, err = h.Pool.Exec(r.Context(), `UPDATE system_settings SET value=$1, updated_at=now() WHERE key='invoice_header_image'`, rel)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.recordAudit(r, "setting.update", "system_setting", nil, strPtr("invoice_header_image"), map[string]string{"value": rel})
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
	}
}

