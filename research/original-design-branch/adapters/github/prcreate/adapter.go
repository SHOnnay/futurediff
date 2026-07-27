package prcreate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const SupportLevelPreviewWithFreshnessCheck = "preview_with_freshness_check"

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

type CreateRequest struct {
	Owner    string
	Repo     string
	Title    string
	Head     string
	Base     string
	BaseSHA  string
	Body     string
	EffectID string
}

type PreparedRequest struct {
	Request      CreateRequest
	Fingerprint  string
	SupportLevel string
	PreviewBody  string
}

type Receipt struct {
	PullNumber int
	HTMLURL    string
	Recovered  bool
}

type CompensationReceipt struct {
	PullNumber int
	HTMLURL    string
	State      string
}

type FreshnessResult struct {
	Fresh      bool
	CurrentSHA string
}

type pull struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
	Head    struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Body string `json:"body"`
}

type branch struct {
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

func (c Client) Prepare(req CreateRequest) PreparedRequest {
	previewBody := strings.TrimSpace(req.Body)
	if req.EffectID != "" {
		marker := fmt.Sprintf("\n\nFutureDiff-Effect: %s", req.EffectID)
		if !strings.Contains(previewBody, marker) {
			previewBody += marker
		}
	}
	payload := map[string]string{
		"title": req.Title,
		"head":  req.Head,
		"base":  req.Base,
		"body":  previewBody,
	}
	if strings.TrimSpace(req.BaseSHA) != "" {
		payload["expected_base_sha"] = req.BaseSHA
	}
	bytes, _ := json.Marshal(payload)
	fingerprint := sha256.Sum256(bytes)
	return PreparedRequest{
		Request:      req,
		Fingerprint:  hex.EncodeToString(fingerprint[:]),
		SupportLevel: SupportLevelPreviewWithFreshnessCheck,
		PreviewBody:  previewBody,
	}
}

func (c Client) Create(ctx context.Context, prepared PreparedRequest) (*Receipt, error) {
	payload := map[string]string{
		"title": prepared.Request.Title,
		"head":  prepared.Request.Head,
		"base":  prepared.Request.Base,
		"body":  prepared.PreviewBody,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal create pr payload: %w", err)
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls", strings.TrimRight(c.baseURL(), "/"), prepared.Request.Owner, prepared.Request.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build create pr request: %w", err)
	}
	c.applyHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("create pr request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		bytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create pr status %d: %s", resp.StatusCode, strings.TrimSpace(string(bytes)))
	}
	var created pull
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("decode create pr response: %w", err)
	}
	return &Receipt{PullNumber: created.Number, HTMLURL: created.HTMLURL}, nil
}

func (c Client) Recover(ctx context.Context, prepared PreparedRequest) (*Receipt, error) {
	query := url.Values{}
	query.Set("state", "open")
	query.Set("head", prepared.Request.Head)
	query.Set("base", prepared.Request.Base)
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls?%s", strings.TrimRight(c.baseURL(), "/"), prepared.Request.Owner, prepared.Request.Repo, query.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build recover request: %w", err)
	}
	c.applyHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("recover pr request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		bytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("recover pr status %d: %s", resp.StatusCode, strings.TrimSpace(string(bytes)))
	}
	var pulls []pull
	if err := json.NewDecoder(resp.Body).Decode(&pulls); err != nil {
		return nil, fmt.Errorf("decode recover response: %w", err)
	}
	marker := fmt.Sprintf("FutureDiff-Effect: %s", prepared.Request.EffectID)
	for _, candidate := range pulls {
		if candidate.Title == prepared.Request.Title && candidate.Head.Ref == prepared.Request.Head && candidate.Base.Ref == prepared.Request.Base {
			if prepared.Request.EffectID == "" || strings.Contains(candidate.Body, marker) {
				return &Receipt{PullNumber: candidate.Number, HTMLURL: candidate.HTMLURL, Recovered: true}, nil
			}
		}
	}
	return nil, fmt.Errorf("no matching pull request found during recovery")
}

func (c Client) CheckBaseFreshness(ctx context.Context, prepared PreparedRequest) (*FreshnessResult, error) {
	if strings.TrimSpace(prepared.Request.BaseSHA) == "" {
		return &FreshnessResult{Fresh: true}, nil
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/branches/%s", strings.TrimRight(c.baseURL(), "/"), prepared.Request.Owner, prepared.Request.Repo, prepared.Request.Base)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build branch freshness request: %w", err)
	}
	c.applyHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("branch freshness request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		bytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("branch freshness status %d: %s", resp.StatusCode, strings.TrimSpace(string(bytes)))
	}
	var current branch
	if err := json.NewDecoder(resp.Body).Decode(&current); err != nil {
		return nil, fmt.Errorf("decode branch freshness response: %w", err)
	}
	return &FreshnessResult{
		Fresh:      current.Commit.SHA == prepared.Request.BaseSHA,
		CurrentSHA: current.Commit.SHA,
	}, nil
}

func (c Client) Close(ctx context.Context, prepared PreparedRequest, receipt *Receipt) (*CompensationReceipt, error) {
	if receipt == nil {
		return nil, fmt.Errorf("github receipt is required for compensation")
	}
	payload := map[string]string{"state": "closed"}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal close pr payload: %w", err)
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", strings.TrimRight(c.baseURL(), "/"), prepared.Request.Owner, prepared.Request.Repo, receipt.PullNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build close pr request: %w", err)
	}
	c.applyHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("close pr request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		bytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("close pr status %d: %s", resp.StatusCode, strings.TrimSpace(string(bytes)))
	}
	var closed struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		State   string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&closed); err != nil {
		return nil, fmt.Errorf("decode close pr response: %w", err)
	}
	return &CompensationReceipt{PullNumber: closed.Number, HTMLURL: closed.HTMLURL, State: closed.State}, nil
}

func (c Client) baseURL() string {
	if strings.TrimSpace(c.BaseURL) == "" {
		return "https://api.github.com"
	}
	return c.BaseURL
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c Client) applyHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}
