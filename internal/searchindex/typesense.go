package searchindex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"mcmods-cn-backend/internal/config"
)

var ErrUnavailable = errors.New("typesense search is unavailable")

type Client struct {
	config config.TypesenseConfig
	http   *http.Client
	ready  atomic.Bool
}

type Field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Facet    bool   `json:"facet,omitempty"`
	Infix    bool   `json:"infix,omitempty"`
	Index    *bool  `json:"index,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

type CollectionSchema struct {
	Name                string  `json:"name"`
	Fields              []Field `json:"fields"`
	DefaultSortingField string  `json:"default_sorting_field,omitempty"`
}

type SearchRequest struct {
	Collection string
	Query      string
	QueryBy    []string
	FilterBy   string
	SortBy     string
	FacetBy    []string
	Infix      []string
	Page       int
	PerPage    int
}

type SearchResult struct {
	IDs    []int64
	Found  int
	Facets map[string]map[string]int
}

func New(cfg config.TypesenseConfig) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Client{config: cfg, http: &http.Client{Timeout: timeout}}
}

func (client *Client) Enabled() bool {
	return client != nil && client.config.Enabled
}

func (client *Client) Ready() bool {
	return client.Enabled() && client.ready.Load()
}

func (client *Client) SetReady(ready bool) {
	if client != nil {
		client.ready.Store(ready)
	}
}

func (client *Client) Alias(kind string) string {
	return strings.TrimSpace(client.config.CollectionPrefix) + "_" + kind
}

func (client *Client) VersionedCollection(kind string) string {
	return client.Alias(kind) + "_v" + strconv.Itoa(projectionSchemaVersion) + "_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}

func (client *Client) Health(ctx context.Context) error {
	if !client.Enabled() {
		return ErrUnavailable
	}
	return client.request(ctx, http.MethodGet, "/health", nil, nil, nil)
}

func (client *Client) AliasTarget(ctx context.Context, alias string) (string, error) {
	var response struct {
		Collection string `json:"collection_name"`
	}
	err := client.request(ctx, http.MethodGet, "/aliases/"+url.PathEscape(alias), nil, nil, &response)
	return response.Collection, err
}

func (client *Client) CreateCollection(ctx context.Context, schema CollectionSchema) error {
	return client.request(ctx, http.MethodPost, "/collections", nil, schema, nil)
}

func (client *Client) CollectionExists(ctx context.Context, collection string) (bool, error) {
	err := client.request(ctx, http.MethodGet, "/collections/"+url.PathEscape(collection), nil, nil, nil)
	if isStatus(err, http.StatusNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (client *Client) DeleteCollection(ctx context.Context, collection string) error {
	err := client.request(ctx, http.MethodDelete, "/collections/"+url.PathEscape(collection), nil, nil, nil)
	if isStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (client *Client) UpsertAlias(ctx context.Context, alias, collection string) error {
	return client.request(ctx, http.MethodPut, "/aliases/"+url.PathEscape(alias), nil,
		map[string]string{"collection_name": collection}, nil)
}

func (client *Client) ImportDocuments(ctx context.Context, collection string, documents []map[string]any) error {
	if len(documents) == 0 {
		return nil
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	for _, document := range documents {
		if err := encoder.Encode(document); err != nil {
			return fmt.Errorf("encode Typesense document: %w", err)
		}
	}
	query := url.Values{"action": {"upsert"}}
	var raw json.RawMessage
	if err := client.requestWithContentType(ctx, http.MethodPost,
		"/collections/"+url.PathEscape(collection)+"/documents/import", query, "text/plain", &body, &raw); err != nil {
		if isStatus(err, http.StatusNotFound) {
			client.SetReady(false)
		}
		return err
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		var result struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
			return fmt.Errorf("decode Typesense import result: %w", err)
		}
		if !result.Success {
			return fmt.Errorf("Typesense rejected a document: %s", result.Error)
		}
	}
	return scanner.Err()
}

func (client *Client) DeleteDocument(ctx context.Context, collection, documentID string) error {
	query := url.Values{"ignore_not_found": {"true"}}
	err := client.request(ctx, http.MethodDelete,
		"/collections/"+url.PathEscape(collection)+"/documents/"+url.PathEscape(documentID), query, nil, nil)
	if isStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (client *Client) Search(ctx context.Context, request SearchRequest) (SearchResult, error) {
	if !client.Ready() {
		return SearchResult{}, ErrUnavailable
	}
	page, perPage := max(1, request.Page), min(max(1, request.PerPage), 250)
	query := url.Values{
		"q":                {request.Query},
		"query_by":         {strings.Join(request.QueryBy, ",")},
		"page":             {strconv.Itoa(page)},
		"per_page":         {strconv.Itoa(perPage)},
		"prefix":           {"true"},
		"num_typos":        {"2"},
		"drop_tokens_mode": {"both_sides:3"},
	}
	if request.FilterBy != "" {
		query.Set("filter_by", request.FilterBy)
	}
	if request.SortBy != "" {
		query.Set("sort_by", request.SortBy)
	}
	if len(request.FacetBy) > 0 {
		query.Set("facet_by", strings.Join(request.FacetBy, ","))
	}
	if len(request.Infix) > 0 {
		query.Set("infix", strings.Join(request.Infix, ","))
	}
	var response struct {
		Found int `json:"found"`
		Hits  []struct {
			Document struct {
				InternalID int64 `json:"internal_id"`
			} `json:"document"`
		} `json:"hits"`
		FacetCounts []struct {
			FieldName string `json:"field_name"`
			Counts    []struct {
				Name  string `json:"value"`
				Count int    `json:"count"`
			} `json:"counts"`
		} `json:"facet_counts"`
	}
	err := client.request(ctx, http.MethodGet,
		"/collections/"+url.PathEscape(client.Alias(request.Collection))+"/documents/search", query, nil, &response)
	if err != nil {
		if errors.Is(err, ErrUnavailable) || isStatus(err, http.StatusNotFound) {
			client.SetReady(false)
		}
		return SearchResult{}, err
	}
	result := SearchResult{Found: response.Found, IDs: make([]int64, 0, len(response.Hits)), Facets: make(map[string]map[string]int)}
	for _, hit := range response.Hits {
		if hit.Document.InternalID > 0 {
			result.IDs = append(result.IDs, hit.Document.InternalID)
		}
	}
	for _, facet := range response.FacetCounts {
		values := make(map[string]int, len(facet.Counts))
		for _, count := range facet.Counts {
			values[count.Name] = count.Count
		}
		result.Facets[facet.FieldName] = values
	}
	return result, nil
}

type statusError struct {
	Status int
	Body   string
}

func (err *statusError) Error() string {
	return fmt.Sprintf("Typesense returned HTTP %d: %s", err.Status, err.Body)
}

func isStatus(err error, status int) bool {
	var responseError *statusError
	return errors.As(err, &responseError) && responseError.Status == status
}

func (client *Client) request(ctx context.Context, method, path string, query url.Values, input any, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	return client.requestWithContentType(ctx, method, path, query, "application/json", body, output)
}

func (client *Client) requestWithContentType(ctx context.Context, method, path string, query url.Values, contentType string, body io.Reader, output any) error {
	if !client.Enabled() {
		return ErrUnavailable
	}
	target := strings.TrimRight(client.config.URL, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	request.Header.Set("X-TYPESENSE-API-KEY", client.config.APIKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "mcmods-cn-backend/search-index")
	if body != nil {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := client.http.Do(request)
	if err != nil {
		client.SetReady(false)
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if readErr != nil {
		return readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseError := &statusError{Status: response.StatusCode, Body: strings.TrimSpace(string(raw))}
		if response.StatusCode >= http.StatusInternalServerError {
			client.SetReady(false)
			return fmt.Errorf("%w: %w", ErrUnavailable, responseError)
		}
		return responseError
	}
	if output == nil || len(raw) == 0 {
		return nil
	}
	if targetRaw, ok := output.(*json.RawMessage); ok {
		*targetRaw = append((*targetRaw)[:0], raw...)
		return nil
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return fmt.Errorf("decode Typesense response: %w", err)
	}
	return nil
}
