package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config bundles the Microsoft Graph OAuth + API endpoints. The defaults
// target the `common` multi-tenant endpoint, which works for any Entra
// tenant whose admin has consented to our app.
type Config struct {
	TenantID     string   // "common" or a GUID
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string // default: ["offline_access","Mail.Read","User.Read"]
	APIBase      string   // default: "https://graph.microsoft.com/v1.0"
}

func (c Config) tenant() string {
	if c.TenantID == "" {
		return "common"
	}
	return c.TenantID
}
func (c Config) scopes() []string {
	if len(c.Scopes) == 0 {
		return []string{"offline_access", "Mail.Read", "User.Read"}
	}
	return c.Scopes
}
func (c Config) apiBase() string {
	if c.APIBase == "" {
		return "https://graph.microsoft.com/v1.0"
	}
	return strings.TrimRight(c.APIBase, "/")
}
func (c Config) authorizeURL() string {
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize", c.tenant())
}
func (c Config) tokenURL() string {
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", c.tenant())
}

// --- Client ---------------------------------------------------------------

type Client struct {
	cfg  Config
	http *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) Config() Config { return c.cfg }

// AuthorizeURL returns the URL to redirect the user to during /connectors/graph/connect.
func (c *Client) AuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("client_id", c.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", c.cfg.RedirectURL)
	q.Set("response_mode", "query")
	q.Set("scope", strings.Join(c.cfg.scopes(), " "))
	q.Set("state", state)
	return c.cfg.authorizeURL() + "?" + q.Encode()
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

// ExchangeCode turns the authorization code into a refresh token.
func (c *Client) ExchangeCode(ctx context.Context, code string) (*tokenResp, error) {
	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", c.cfg.RedirectURL)
	form.Set("scope", strings.Join(c.cfg.scopes(), " "))
	return c.postToken(ctx, form)
}

// Refresh swaps a stored refresh token for a short-lived access token
// (and, usually, a rotated refresh token).
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*tokenResp, error) {
	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")
	form.Set("scope", strings.Join(c.cfg.scopes(), " "))
	return c.postToken(ctx, form)
}

func (c *Client) postToken(ctx context.Context, form url.Values) (*tokenResp, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.tokenURL(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("graph token %s: %s", resp.Status, string(buf))
	}
	var t tokenResp
	if err := json.Unmarshal(buf, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// --- Graph API: /me, /me/messages ----------------------------------------

type UserInfo struct {
	ID                string `json:"id"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	DisplayName       string `json:"displayName"`
}

func (c *Client) Me(ctx context.Context, accessToken string) (*UserInfo, error) {
	var u UserInfo
	err := c.get(ctx, accessToken, "/me", &u)
	return &u, err
}

// Message is a subset of the Graph message object — the fields we need
// to populate an email.v1 document.
type Message struct {
	ID               string `json:"id"`
	Subject          string `json:"subject"`
	BodyPreview      string `json:"bodyPreview"`
	ReceivedDateTime string `json:"receivedDateTime"`
	From             struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"from"`
	ToRecipients []recipient `json:"toRecipients"`
	CcRecipients []recipient `json:"ccRecipients"`
	Body         struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
}

type recipient struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

// ListMessages returns up to `top` most-recently-received messages newer
// than `since` (zero time = no lower bound).
func (c *Client) ListMessages(ctx context.Context, accessToken string, since time.Time, top int) ([]Message, error) {
	if top <= 0 || top > 100 {
		top = 25
	}
	q := url.Values{}
	q.Set("$top", fmt.Sprintf("%d", top))
	q.Set("$orderby", "receivedDateTime desc")
	q.Set("$select", "id,subject,bodyPreview,receivedDateTime,from,toRecipients,ccRecipients,body")
	if !since.IsZero() {
		q.Set("$filter", "receivedDateTime ge "+since.UTC().Format(time.RFC3339))
	}
	var resp struct {
		Value []Message `json:"value"`
	}
	if err := c.get(ctx, accessToken, "/me/messages?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

func (c *Client) get(ctx context.Context, token, path string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.apiBase()+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("graph %s: %s: %s", path, resp.Status, string(buf))
	}
	if out != nil {
		return json.Unmarshal(buf, out)
	}
	return nil
}
