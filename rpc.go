package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type RPCClient struct {
	url    string
	auth   string
	client *http.Client
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	ID     string           `json:"id"`
	Result json.RawMessage  `json:"result"`
	Error  *rpcError        `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

func NewRPCClient(url, user, password string) *RPCClient {
	c := &RPCClient{
		url:    url,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	if user != "" && password != "" {
		c.auth = base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	}
	return c
}

func (c *RPCClient) Call(method string, params []any) (json.RawMessage, error) {
	p, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	reqBody := rpcRequest{
		JSONRPC: "1.0",
		ID:      "legacy-miner",
		Method:  method,
		Params:  p,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest("POST", c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.auth != "" {
		httpReq.Header.Set("Authorization", "Basic "+c.auth)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("RPC call %s: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("RPC call %s: read body: %w", method, err)
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("RPC call %s: parse response: %w", method, err)
	}
	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}
	return rpcResp.Result, nil
}

type BlockTemplate struct {
	Height      int    `json:"height"`
	PrevHash    string `json:"previoushash"`
	Bits        string `json:"bits"`
	MerkleRoot  string `json:"merkleroot"`
	Timestamp   uint32 `json:"time"`
	Transactions json.RawMessage `json:"transactions"`
	MempoolSize int    `json:"mempoolsize"`
	Hex         string `json:"hex"`
}

func (c *RPCClient) GetBlockTemplate(pubKeyHash string) (*BlockTemplate, error) {
	params := []any{}
	if pubKeyHash != "" {
		params = []any{pubKeyHash}
	}
	result, err := c.Call("getblocktemplate", params)
	if err != nil {
		return nil, err
	}
	var tmpl BlockTemplate
	if err := json.Unmarshal(result, &tmpl); err != nil {
		return nil, fmt.Errorf("parse getblocktemplate: %w", err)
	}
	return &tmpl, nil
}

func (c *RPCClient) SubmitBlock(hexStr string) error {
	result, err := c.Call("submitblock", []any{hexStr})
	if err != nil {
		return err
	}
	var s string
	if json.Unmarshal(result, &s) == nil && s != "" {
		return errors.New(s)
	}
	return nil
}
