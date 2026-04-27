// Package pymesclient implementa el cliente HTTP a Pymes Core API.
package pymesclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/devpablocristo/core/http/go/httpclient"

	domain "github.com/devpablocristo/companion/internal/watchers/usecases/domain"
)

// Config configura timeout y retry del cliente. Los defaults se aplican si
// se pasan ceros o se usa NewClient simple.
type Config struct {
	Timeout       time.Duration // por request HTTP. Default 15s.
	MaxRetries    int           // GETs idempotentes. Default 2 (3 intentos totales).
	BackoffBase   time.Duration // base del backoff exponencial. Default 200ms.
	BackoffMax    time.Duration // techo del backoff por intento. Default 2s.
}

func (c Config) withDefaults() Config {
	if c.Timeout <= 0 {
		c.Timeout = 15 * time.Second
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = 0
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 2
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = 200 * time.Millisecond
	}
	if c.BackoffMax <= 0 {
		c.BackoffMax = 2 * time.Second
	}
	return c
}

// Client es un cliente HTTP para Pymes Core API.
type Client struct {
	caller *httpclient.Caller
	cfg    Config
}

// NewClient crea un nuevo cliente con defaults razonables.
func NewClient(baseURL, apiKey string) *Client {
	return NewClientWithConfig(baseURL, apiKey, Config{})
}

// NewClientWithConfig permite override de timeout/retry desde wire.
func NewClientWithConfig(baseURL, apiKey string, cfg Config) *Client {
	cfg = cfg.withDefaults()
	h := make(http.Header)
	h.Set("X-API-Key", apiKey)
	return &Client{
		caller: &httpclient.Caller{
			BaseURL: baseURL,
			Header:  h,
			HTTP:    &http.Client{Timeout: cfg.Timeout},
		},
		cfg: cfg,
	}
}

// doGet aplica retry con backoff sobre errores de red y 5xx. Es seguro
// porque GET es idempotente. POST no se reintenta — riesgo de doble-send.
func (c *Client) doGet(ctx context.Context, path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		st, raw, err := c.caller.DoJSON(ctx, http.MethodGet, path, nil)
		if err == nil && st < 300 {
			return raw, nil
		}
		// Construir el error de este intento.
		if err != nil {
			lastErr = fmt.Errorf("pymes GET %s: %w", path, err)
		} else {
			lastErr = fmt.Errorf("pymes GET %s: status %d", path, st)
		}
		// 4xx → no reintentar. 5xx o net err → reintentar si hay budget.
		if err == nil && st < 500 {
			return nil, lastErr
		}
		if attempt == c.cfg.MaxRetries {
			break
		}
		// Backoff exponencial con techo. Respetar ctx.
		wait := c.cfg.BackoffBase << attempt
		if wait > c.cfg.BackoffMax {
			wait = c.cfg.BackoffMax
		}
		slog.Warn("pymes GET retry", "path", path, "attempt", attempt+1, "wait", wait, "error", lastErr)
		select {
		case <-ctx.Done():
			return nil, errors.Join(lastErr, ctx.Err())
		case <-time.After(wait):
		}
	}
	return nil, lastErr
}

// doPost no reintenta — POSTs (WhatsApp send) no son idempotentes y un
// retry podría duplicar el mensaje al cliente.
func (c *Client) doPost(ctx context.Context, path string, payload any) ([]byte, error) {
	st, raw, err := c.caller.DoJSON(ctx, http.MethodPost, path, payload)
	if err != nil {
		return nil, fmt.Errorf("pymes POST %s: %w", path, err)
	}
	if st >= 300 {
		return nil, fmt.Errorf("pymes POST %s: status %d", path, st)
	}
	return raw, nil
}

func withOrgQuery(path, orgID string) string {
	orgID = url.QueryEscape(orgID)
	if orgID == "" {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "org_id=" + orgID
}

func parseItems(data []byte) ([]domain.PymesItem, error) {
	var wrapper struct {
		Items []domain.PymesItem `json:"items"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse items: %w", err)
	}
	return wrapper.Items, nil
}

// GetStaleWorkOrders consulta OTs que llevan más de thresholdDays sin avanzar.
func (c *Client) GetStaleWorkOrders(ctx context.Context, orgID string, thresholdDays int) ([]domain.PymesItem, error) {
	data, err := c.doGet(ctx, withOrgQuery(fmt.Sprintf("/v1/work-orders?status=in_progress&stale_days=%d", thresholdDays), orgID))
	if err != nil {
		return nil, err
	}
	return parseItems(data)
}

// GetUnconfirmedAppointments consulta turnos no confirmados.
func (c *Client) GetUnconfirmedAppointments(ctx context.Context, orgID string, hoursBefore int) ([]domain.PymesItem, error) {
	data, err := c.doGet(ctx, withOrgQuery(fmt.Sprintf("/v1/appointments?confirmed=false&upcoming_hours=%d", hoursBefore), orgID))
	if err != nil {
		return nil, err
	}
	return parseItems(data)
}

// GetLowStockItems consulta productos con stock bajo.
func (c *Client) GetLowStockItems(ctx context.Context, orgID string, thresholdUnits int) ([]domain.PymesItem, error) {
	data, err := c.doGet(ctx, withOrgQuery(fmt.Sprintf("/v1/inventory/low-stock?threshold=%d", thresholdUnits), orgID))
	if err != nil {
		return nil, err
	}
	return parseItems(data)
}

// GetInactiveCustomers consulta clientes inactivos.
func (c *Client) GetInactiveCustomers(ctx context.Context, orgID string, thresholdMonths int) ([]domain.PymesItem, error) {
	data, err := c.doGet(ctx, withOrgQuery(fmt.Sprintf("/v1/customers?inactive_months=%d", thresholdMonths), orgID))
	if err != nil {
		return nil, err
	}
	return parseItems(data)
}

// GetRevenueComparison consulta comparación de facturación mensual.
func (c *Client) GetRevenueComparison(ctx context.Context, orgID string) (*domain.RevenueComparison, error) {
	data, err := c.doGet(ctx, withOrgQuery("/v1/dashboard/revenue?compare=previous_month", orgID))
	if err != nil {
		return nil, err
	}
	var result domain.RevenueComparison
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse revenue comparison: %w", err)
	}
	return &result, nil
}

// SendWhatsAppTemplate envía un template de WhatsApp.
func (c *Client) SendWhatsAppTemplate(ctx context.Context, orgID, partyID, templateName string, params map[string]string) error {
	_, err := c.doPost(ctx, "/v1/whatsapp/send/template", map[string]any{
		"org_id": orgID, "party_id": partyID, "template_name": templateName, "language": "es", "params": params,
	})
	return err
}

// SendWhatsAppText envía un mensaje de texto por WhatsApp.
func (c *Client) SendWhatsAppText(ctx context.Context, orgID, partyID, body string) error {
	_, err := c.doPost(ctx, "/v1/whatsapp/send/text", map[string]any{
		"org_id": orgID, "party_id": partyID, "body": body,
	})
	return err
}
