package acl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a minimal HTTP client for OpenFGA. The SDK is avoided to keep
// the gateway's dependency surface small — we only need half a dozen
// endpoints (§3.3).
type Client struct {
	baseURL string
	http    *http.Client
	storeID string
	modelID string
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// StoreID / ModelID return the currently bootstrapped identifiers.
func (c *Client) StoreID() string { return c.storeID }
func (c *Client) ModelID() string { return c.modelID }

// setIDs is called by bootstrap.go after the store + model are ready.
func (c *Client) setIDs(store, model string) { c.storeID, c.modelID = store, model }

// Tuple is a subject-relation-object triple.
type Tuple struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
}

// --- low-level HTTP ------------------------------------------------------

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("fga %s %s: %s: %s", method, path, resp.Status, string(buf))
	}
	if out != nil && len(buf) > 0 {
		return json.Unmarshal(buf, out)
	}
	return nil
}

// --- stores --------------------------------------------------------------

type storeListResp struct {
	Stores []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"stores"`
}

func (c *Client) FindStoreByName(ctx context.Context, name string) (string, error) {
	var r storeListResp
	if err := c.do(ctx, http.MethodGet, "/stores", nil, &r); err != nil {
		return "", err
	}
	for _, s := range r.Stores {
		if s.Name == name {
			return s.ID, nil
		}
	}
	return "", nil
}

type storeCreateResp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) CreateStore(ctx context.Context, name string) (string, error) {
	var r storeCreateResp
	if err := c.do(ctx, http.MethodPost, "/stores", map[string]string{"name": name}, &r); err != nil {
		return "", err
	}
	return r.ID, nil
}

// --- authorization model -------------------------------------------------

type writeModelResp struct {
	AuthorizationModelID string `json:"authorization_model_id"`
}

func (c *Client) WriteModel(ctx context.Context, storeID, modelJSON string) (string, error) {
	var body any
	if err := json.Unmarshal([]byte(modelJSON), &body); err != nil {
		return "", fmt.Errorf("parse model json: %w", err)
	}
	var r writeModelResp
	if err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/stores/%s/authorization-models", storeID), body, &r); err != nil {
		return "", err
	}
	return r.AuthorizationModelID, nil
}

// --- check ---------------------------------------------------------------

type checkReq struct {
	AuthorizationModelID string `json:"authorization_model_id"`
	TupleKey             struct {
		User     string `json:"user"`
		Relation string `json:"relation"`
		Object   string `json:"object"`
	} `json:"tuple_key"`
}
type checkResp struct {
	Allowed bool `json:"allowed"`
}

func (c *Client) Check(ctx context.Context, user, relation, object string) (bool, error) {
	if c.storeID == "" {
		return false, errors.New("fga client not bootstrapped")
	}
	req := checkReq{AuthorizationModelID: c.modelID}
	req.TupleKey.User = user
	req.TupleKey.Relation = relation
	req.TupleKey.Object = object
	var r checkResp
	if err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/stores/%s/check", c.storeID), req, &r); err != nil {
		return false, err
	}
	return r.Allowed, nil
}

// --- write / delete tuples ----------------------------------------------

type writeReq struct {
	AuthorizationModelID string       `json:"authorization_model_id"`
	Writes               *tupleKeys   `json:"writes,omitempty"`
	Deletes              *tupleKeys   `json:"deletes,omitempty"`
}
type tupleKeys struct {
	TupleKeys []Tuple `json:"tuple_keys"`
}

func (c *Client) Write(ctx context.Context, tuples ...Tuple) error {
	if c.storeID == "" {
		return errors.New("fga client not bootstrapped")
	}
	req := writeReq{
		AuthorizationModelID: c.modelID,
		Writes:               &tupleKeys{TupleKeys: tuples},
	}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/stores/%s/write", c.storeID), req, nil)
}

func (c *Client) Delete(ctx context.Context, tuples ...Tuple) error {
	if c.storeID == "" {
		return errors.New("fga client not bootstrapped")
	}
	req := writeReq{
		AuthorizationModelID: c.modelID,
		Deletes:              &tupleKeys{TupleKeys: tuples},
	}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/stores/%s/write", c.storeID), req, nil)
}

// --- list-objects --------------------------------------------------------

type listObjectsReq struct {
	AuthorizationModelID string `json:"authorization_model_id"`
	Type                 string `json:"type"`
	Relation             string `json:"relation"`
	User                 string `json:"user"`
}
type listObjectsResp struct {
	Objects []string `json:"objects"`
}

// ListObjects returns all object IDs of `objType` on which `user` has
// `relation`. Objects come back as fully-qualified "type:id" strings.
func (c *Client) ListObjects(ctx context.Context, user, relation, objType string) ([]string, error) {
	if c.storeID == "" {
		return nil, errors.New("fga client not bootstrapped")
	}
	req := listObjectsReq{
		AuthorizationModelID: c.modelID,
		Type:                 objType,
		Relation:             relation,
		User:                 user,
	}
	var r listObjectsResp
	if err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/stores/%s/list-objects", c.storeID), req, &r); err != nil {
		return nil, err
	}
	return r.Objects, nil
}
