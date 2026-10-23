package bmw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	apiServer  = "https://api-cardata.bmwgroup.com"
	apiVersion = "v1"
)

// Client is the BMW CarData API HTTP client.
type Client struct {
	auth *Auth
	http *http.Client
}

// NewClient creates a BMW CarData API client backed by auth.
func NewClient(auth *Auth) *Client {
	return &Client{
		auth: auth,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	token, err := c.auth.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth token: %w", err)
	}

	var req *http.Request
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req, err = http.NewRequestWithContext(ctx, method, apiServer+path, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, apiServer+path, nil)
		if err != nil {
			return nil, err
		}
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-version", apiVersion)
	return c.http.Do(req)
}

type vehicleMappingDto struct {
	Vin         *string `json:"vin"`
	MappingType *string `json:"mappingType"`
}

func (c *Client) getMappings(ctx context.Context) ([]vehicleMappingDto, error) {
	resp, err := c.do(ctx, "GET", "/customers/vehicles/mappings", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getMappings: %s", resp.Status)
	}
	var result []vehicleMappingDto
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

type containerDto struct {
	ContainerId *string `json:"containerId"`
	Name        *string `json:"name"`
}

func (c *Client) listContainers(ctx context.Context) ([]containerDto, error) {
	resp, err := c.do(ctx, "GET", "/customers/containers", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listContainers: %s", resp.Status)
	}
	var result struct {
		Containers []containerDto `json:"containers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Containers, nil
}

func (c *Client) createContainer(ctx context.Context, name string, descriptors []string) (string, error) {
	body := map[string]interface{}{
		"name":                 name,
		"purpose":              name,
		"technicalDescriptors": descriptors,
	}
	resp, err := c.do(ctx, "POST", "/customers/containers", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", responseError("create container", resp.Status, body)
	}

	var result struct {
		ContainerId *string `json:"containerId"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) == nil && result.ContainerId != nil && *result.ContainerId != "" {
		return *result.ContainerId, nil
	}

	// Fallback: re-list to find the newly created container
	containers, err := c.listContainers(ctx)
	if err != nil {
		return "", fmt.Errorf("createContainer fallback list: %w", err)
	}
	for _, ct := range containers {
		if ct.Name != nil && *ct.Name == name && ct.ContainerId != nil {
			return *ct.ContainerId, nil
		}
	}
	return "", fmt.Errorf("container %q not found after creation", name)
}

// TelematicEntry is a single BMW telematic data point.
type TelematicEntry struct {
	Value     *string `json:"value"`
	Unit      *string `json:"unit"`
	Timestamp *string `json:"timestamp"`
}

// getBasicData retrieves static vehicle metadata once; it is persisted with the telemetry cache.
func (c *Client) getBasicData(ctx context.Context, vin string) (map[string]any, error) {
	resp, err := c.do(ctx, http.MethodGet, "/customers/vehicles/"+vin+"/basicData", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getBasicData: %s", resp.Status)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) getTelematicData(ctx context.Context, vin, containerID string) (map[string]TelematicEntry, error) {
	path := fmt.Sprintf("/customers/vehicles/%s/telematicData?containerId=%s", vin, containerID)
	resp, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getTelematicData: %s", resp.Status)
	}
	var result struct {
		TelematicData map[string]TelematicEntry `json:"telematicData"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.TelematicData, nil
}
